package livecodex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var safeFact = regexp.MustCompile(`^live_failure\.(stage=(startup|fixture|thread-start|turn-start|protocol-request|admission|terminal-turn|timeout|unknown|assertion)|codex_cli_version=codex-cli [0-9]+\.[0-9]+\.[0-9]+|generated_baseline\.(ref=rust-v[0-9]+\.[0-9]+\.[0-9]+|commit=[0-9a-f]{40})|runtime_app_server\.observed=(true|false)|runtime_compatibility=unknown|protocol\.(method=(thread|turn)/[a-zA-Z]+|code=-?[0-9]+)|(thread_id_present|turn_id_present)=(true|false)|turn_status=(completed|failed|interrupted|inProgress)|native_turn_error=(absent)|native_turn_error\.(codex_error_info=[a-zA-Z]+|http_status=[0-9]+)|thread\.(model_matches_fixture|provider_matches_fixture)=(true|false))$`)

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
				if index := strings.Index(line, "live_failure."); index >= 0 {
					fact := line[index:]
					if len(fact) <= 256 && safeFact.MatchString(fact) {
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
