package livecodex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var safeFact = regexp.MustCompile(`^live_failure\.(stage=(startup|fixture|thread-start|turn-start|protocol-request|admission|terminal-turn|timeout|unknown|assertion|cleanup)|codex_cli_version=codex-cli [0-9]+\.[0-9]+\.[0-9]+|generated_baseline\.(ref=rust-v[0-9]+\.[0-9]+\.[0-9]+|commit=[0-9a-f]{40})|runtime_app_server\.observed=(true|false)|runtime_compatibility=unknown|protocol\.(method=(thread|turn)/[a-zA-Z]+|code=-?[0-9]+)|(thread_id_present|turn_id_present)=(true|false)|turn_status=(completed|failed|interrupted|inProgress)|native_turn_error=(absent)|native_turn_error\.(codex_error_info=[a-zA-Z]+|http_status=[0-9]+|message_signature=(unknown|content_filter_message|request_timeout_message|child_timeout_message|child_stdio_message|sandbox_executable_message|stream_disconnected_prefix|fatal_prefix|missing_environment_prefix))|thread\.(model_matches_fixture|provider_matches_fixture)=(true|false))$`)

var safeStoryFact = regexp.MustCompile(`^live_story\.stage=(structured-call|persistent-start|resume|fork|fork-turn) outcome=(started|completed)$`)

var safeExecutorFact = regexp.MustCompile(`^live_executor\.stage=(configure|preflight|start|resume|readonly_review|readonly_probe) outcome=(started|completed|failed) diagnostic=(none|completed|positive_control_failed|write_denial_unverified|write_not_observed|read_assertion|write_denied_read_verified|configuration_failed|file_assertion|sandbox_write_unverified|sandbox_unavailable|command_failed|runtime_warning|command_succeeded_write_unverified|endpoint_not_found|authentication_failed|provider_failed|execution_failed|missing_session|session_mismatch|invalid_event_stream|event_too_large|event_decode_failed|event_sequence_invalid|event_read_failed|cancelled)$`)

// testing.TempDir reports errors through testing.go, not the caller's t.Log.
var tempDirCleanup = regexp.MustCompile(`^\s*testing\.go:[0-9]+: TempDir RemoveAll cleanup: (.*)$`)

var safeScenario = regexp.MustCompile(`^TestLiveCodex[a-zA-Z0-9_./-]*$`)

// CheckResults rejects successful process exits that did not prove live execution.
// Raw output is private; only fixture-owned bounded facts are forwarded.
func CheckResults(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	started, passed := map[string]bool{}, map[string]bool{}
	failed := false
	for {
		var event struct{ Action, Test, Output string }
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return errors.New("invalid live test event stream")
		}
		if event.Test == "" && event.Action == "fail" {
			failed = true
		}
		if !strings.HasPrefix(event.Test, "TestLiveCodex") {
			continue
		}
		switch event.Action {
		case "run":
			if started[event.Test] {
				failed = true
			}
			started[event.Test] = true
		case "pass":
			passed[event.Test] = true
		case "skip", "fail":
			failed = true
		case "output":
			for _, line := range strings.Split(event.Output, "\n") {
				if match := tempDirCleanup.FindStringSubmatch(line); match != nil {
					operation, reason := cleanupErrorFacts(match[1])
					fmt.Fprintf(output, "live_harness.cleanup_signature=tempdir_removeall operation=%s reason=%s\n", operation, reason)
				}

				// t.Log's file/line prefix may itself contain "live_".
				if index := strings.LastIndex(line, "live_"); index >= 0 {
					fact := line[index:]
					if len(fact) <= 256 && (safeFact.MatchString(fact) || safeStoryFact.MatchString(fact) || safeExecutorFact.MatchString(fact)) {
						fmt.Fprintln(output, fact)
					}
				}
			}
		}
		if len(event.Test) <= 128 && safeScenario.MatchString(event.Test) && (event.Action == "run" || event.Action == "pass" || event.Action == "fail" || event.Action == "skip") {
			fmt.Fprintf(output, "live_scenario=%s live_test_result=%s\n", event.Test, event.Action)
		}
	}
	if len(started) == 0 {
		return errors.New("no live scenarios executed")
	}
	for name := range started {
		if !passed[name] {
			failed = true
		}
	}
	if failed {
		return errors.New("live scenarios failed, skipped, repeated, or did not complete")
	}
	fmt.Fprintf(output, "live_tests_completed=%d\n", len(started))
	return nil
}

// cleanupErrorFacts retains the operation and OS error from os.PathError's
// "op path: error" format. Paths and unknown error text can contain credentials
// or transcript content, so neither is forwarded. These are diagnostic facts,
// never an alternative source of scenario acceptance.
func cleanupErrorFacts(payload string) (operation, reason string) {
	operation, reason = "unknown", "unknown"
	op, _, ok := strings.Cut(payload, " ")
	if !ok {
		return operation, reason
	}
	switch op {
	case "RemoveAll", "remove", "unlinkat", "open", "openfdat", "readdirnames":
		operation = op
	}
	separator := strings.LastIndex(payload, ": ")
	if separator < 0 {
		return operation, reason
	}
	switch payload[separator+2:] {
	case "directory not empty":
		reason = "directory_not_empty"
	case "permission denied", "access is denied":
		reason = "permission_denied"
	case "operation not permitted":
		reason = "operation_not_permitted"
	case "device or resource busy":
		reason = "resource_busy"
	case "read-only file system":
		reason = "read_only_filesystem"
	case "no such file or directory":
		reason = "not_found"
	case "input/output error":
		reason = "io_error"
	case "invalid argument":
		reason = "invalid_argument"
	}
	return operation, reason
}
