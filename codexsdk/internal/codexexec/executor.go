// Package codexexec runs the official CLI directly against a native Responses
// provider. It reports process facts; callers own repair and acceptance policy.
package codexexec

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Workspace, StateDir, Binary, Version, BaseURL, APIKey, Model, Reasoning string
}
type Result struct {
	SessionID, Status, Diagnostic, FinalMessagePath string
	ToolDiagnostic                                  string
}
type Executor struct {
	config Config
	home   string
}

var httpStatusPattern = regexp.MustCompile(`(?i)(?:^|status(?: code)?[ :]+|http[ /a-z0-9.]*[ :]+)([45][0-9]{2})\b`)

var runtimeVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var sessionPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// New creates a fresh home and verifies the installed release. Version must be
// the exact stable release resolved from official latest once for this run.
func New(ctx context.Context, cfg Config) (*Executor, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.APIKey == "" || strings.ContainsFunc(cfg.APIKey, unicode.IsSpace) {
		return nil, errors.New("invalid provider credential")
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.BaseURL, "#") || strings.HasSuffix(u.Path, "/responses") {
		return nil, errors.New("invalid Responses API base URL")
	}
	if !runtimeVersionPattern.MatchString(cfg.Version) || cfg.Model == "" || cfg.Reasoning == "" {
		return nil, errors.New("exact stable runtime version, model and reasoning are required")
	}
	if cfg.Binary == "" {
		cfg.Binary = "codex"
	}
	if cfg.Workspace == "" {
		return nil, errors.New("workspace directory is required")
	}
	cfg.Workspace, err = filepath.Abs(cfg.Workspace)
	if err != nil {
		return nil, errors.New("invalid workspace")
	}
	info, err := os.Stat(cfg.Workspace)
	if err != nil || !info.IsDir() {
		return nil, errors.New("workspace directory is required")
	}
	home, err := os.MkdirTemp(cfg.StateDir, "codex-exec-")
	if err != nil {
		return nil, errors.New("cannot create isolated Codex home")
	}
	e := &Executor{config: cfg, home: home}
	workspaceReal, workspaceErr := filepath.EvalSymlinks(cfg.Workspace)
	homeReal, homeErr := filepath.EvalSymlinks(home)
	relative, relativeErr := filepath.Rel(workspaceReal, homeReal)
	if workspaceErr != nil || homeErr != nil || relativeErr != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		_ = e.Close()
		return nil, errors.New("Codex home must be outside the repair workspace")
	}
	fail := func(message string) (*Executor, error) { _ = e.Close(); return nil, errors.New(message) }
	native := map[string]any{
		"model": cfg.Model, "model_reasoning_effort": cfg.Reasoning, "model_provider": "llm-go-exec",
		"approval_policy": "never", "sandbox_mode": "workspace-write",
		"model_providers": map[string]any{"llm-go-exec": map[string]any{
			"name": "llm-go direct executor", "base_url": cfg.BaseURL, "env_key": "AZURE_OPENAI_API_KEY", "wire_api": "responses", "requires_openai_auth": false, "supports_websockets": false,
		}},
	}
	data, err := toml.Marshal(native)
	if err != nil {
		return fail("cannot encode native configuration")
	}
	if os.WriteFile(filepath.Join(home, "config.toml"), data, 0600) != nil {
		return fail("cannot write native configuration")
	}
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := e.command(versionCtx, "--version")
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "codex-cli "+cfg.Version {
		return fail("installed Codex does not match selected release")
	}
	return e, nil
}

func (e *Executor) Close() error { return os.RemoveAll(e.home) }
func (e *Executor) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, e.config.Binary, args...)
	cmd.Dir = e.config.Workspace
	// Deliberately do not inherit repository-write credentials or ambient Codex auth.
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "LANG", "LC_ALL"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+e.home, "AZURE_OPENAI_API_KEY="+e.config.APIKey)
	cmd.WaitDelay = time.Second
	configureCancellation(cmd)
	return cmd
}

// Preflight proves that the selected CLI/provider/model can complete a turn and
// actually execute a bounded sandbox write. Prose claiming success is insufficient.
// Its session is intentionally separate from the repair session.
func (e *Executor) Preflight(ctx context.Context) error {
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return errors.New("preflight nonce unavailable")
	}
	token := hex.EncodeToString(nonce)
	name := ".codex-preflight-" + token
	path := filepath.Join(e.config.Workspace, name)
	defer os.Remove(path)
	result, err := e.Run(ctx, fmt.Sprintf("Sandbox readiness check only. Execute a shell command that writes exactly %s (no newline) into the file %s in the current workspace. Do not change any other file. Then finish.", token, name), "")
	if err != nil {
		return fmt.Errorf("preflight: %s", result.Diagnostic)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(token)) {
		if result.ToolDiagnostic != "" {
			return errors.New("preflight: " + result.ToolDiagnostic)
		}
		return errors.New("preflight: sandbox_write_unverified")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != token {
		if result.ToolDiagnostic != "" {
			return errors.New("preflight: " + result.ToolDiagnostic)
		}
		return errors.New("preflight: sandbox_write_unverified")
	}
	return nil
}

// Run executes exactly one turn. A nonempty sessionID must be the exact UUID
// observed from an earlier run. No provider retry or most-recent-session lookup.
func (e *Executor) Run(ctx context.Context, prompt, sessionID string) (Result, error) {
	result := Result{Status: "failed", Diagnostic: "execution_failed"}
	if sessionID != "" && !sessionPattern.MatchString(sessionID) {
		result.Diagnostic = "invalid_session"
		return result, errors.New(result.Diagnostic)
	}
	f, err := os.CreateTemp(e.home, "final-*.md")
	if err != nil {
		return result, errors.New("cannot create final-message file")
	}
	_ = f.Close()
	result.FinalMessagePath = f.Name()
	// Pin trust-sensitive values at CLI precedence as well as in the isolated
	// home: candidate project config must not redirect the provider credential
	// or widen the sandbox on a later repair turn.
	provider := fmt.Sprintf(`{name="llm-go direct executor",base_url=%s,env_key="AZURE_OPENAI_API_KEY",wire_api="responses",requires_openai_auth=false,supports_websockets=false}`, strconv.Quote(e.config.BaseURL))
	args := []string{"exec", "--sandbox", "workspace-write", "--json", "--output-last-message", f.Name(),
		"-c", `approval_policy="never"`,
		"-c", `model_provider="llm-go-exec"`,
		"-c", "model=" + strconv.Quote(e.config.Model),
		"-c", "model_reasoning_effort=" + strconv.Quote(e.config.Reasoning),
		"-c", "model_providers.llm-go-exec=" + provider,
		"-c", "sandbox_workspace_write.network_access=false",
		"-c", "sandbox_workspace_write.writable_roots=[]",
		"-c", "sandbox_workspace_write.exclude_tmpdir_env_var=true",
		"-c", "sandbox_workspace_write.exclude_slash_tmp=true",
		"-c", `shell_environment_policy.inherit="core"`,
		"-c", `shell_environment_policy.ignore_default_excludes=false`,
		"-c", `shell_environment_policy.exclude=["AZURE_OPENAI_API_KEY"]`,
	}
	if sessionID != "" {
		args = append(args, "resume", sessionID, "-")
	} else {
		args = append(args, "-")
	}
	cmd := e.command(ctx, args...)
	cmd.Stdin = strings.NewReader(prompt)
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	diagnostics := &boundedBuffer{}
	cmd.Stderr = diagnostics
	type parsed struct {
		result Result
		err    error
	}
	done := make(chan parsed, 1)
	go func() { r, err := parseEvents(reader, sessionID); done <- parsed{r, err} }()
	runErr := cmd.Run()
	_ = writer.Close()
	parsedResult := <-done
	_ = reader.Close()
	result.SessionID = parsedResult.result.SessionID
	result.ToolDiagnostic = parsedResult.result.ToolDiagnostic
	if ctx.Err() != nil {
		result.Status = "cancelled"
		result.Diagnostic = "cancelled"
	} else if parsedResult.err != nil {
		result.Diagnostic = parsedResult.result.Diagnostic
		if classified := classify(diagnostics.String()); classified != "execution_failed" {
			result.Diagnostic = classified
		} else if result.Diagnostic == "execution_failed" {
			result.Diagnostic = classify(diagnostics.String())
		}
	} else if runErr != nil {
		result.Diagnostic = classify(diagnostics.String())
	} else {
		result.Status = "completed"
		result.Diagnostic = "completed"
		return result, nil
	}
	if result.Diagnostic == "endpoint_not_found" || result.Diagnostic == "authentication_failed" || result.Diagnostic == "provider_failed" {
		result.Status = "blocked"
	}
	return result, errors.New(result.Diagnostic)
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if left := 16384 - b.Len(); left > 0 {
		_, _ = b.Buffer.Write(p[:min(left, n)])
	}
	return n, nil
}
func classify(s string) string {
	s = strings.ToLower(s)
	status := ""
	if match := httpStatusPattern.FindStringSubmatch(s); len(match) == 2 {
		status = match[1]
	}
	switch {
	case status == "404":
		return "endpoint_not_found"
	case status == "401" || status == "403" || strings.Contains(s, "unauthorized") || strings.Contains(s, "authentication"):
		return "authentication_failed"
	case status == "429" || strings.HasPrefix(status, "5") || strings.Contains(s, "capacity") || strings.Contains(s, "connection") || strings.Contains(s, "stream disconnected"):
		return "provider_failed"
	default:
		return "execution_failed"
	}
}

func parseEvents(r io.Reader, expected string) (Result, error) {
	result := Result{Status: "failed", Diagnostic: "execution_failed"}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	started, completed, failed := false, false, false
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Item     struct {
				Type     string `json:"type"`
				Status   string `json:"status"`
				Message  string `json:"message"`
				ExitCode *int   `json:"exit_code"`
				Output   string `json:"aggregated_output"`
			} `json:"item"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			failed = true
			result.Diagnostic = "invalid_event_stream"
			continue
		}
		switch event.Type {
		case "thread.started":
			if started || !sessionPattern.MatchString(event.ThreadID) || (expected != "" && expected != event.ThreadID) {
				failed = true
				result.Diagnostic = "session_mismatch"
			} else {
				started = true
				result.SessionID = event.ThreadID
			}
		case "item.completed":
			if event.Item.Type == "command_execution" {
				if event.Item.Status == "failed" || event.Item.Status == "declined" || (event.Item.ExitCode != nil && *event.Item.ExitCode != 0) {
					result.ToolDiagnostic = "command_failed"
					lower := strings.ToLower(event.Item.Output)
					if strings.Contains(lower, "bwrap") || strings.Contains(lower, "sandbox") || strings.Contains(lower, "operation not permitted") {
						result.ToolDiagnostic = "sandbox_unavailable"
					}
				} else if event.Item.ExitCode != nil && *event.Item.ExitCode == 0 && result.ToolDiagnostic == "" {
					result.ToolDiagnostic = "command_succeeded_write_unverified"
				}
			}
			if event.Item.Type == "error" {
				result.ToolDiagnostic = "runtime_warning"
				lower := strings.ToLower(event.Item.Message)
				if strings.Contains(lower, "bwrap") || strings.Contains(lower, "sandbox") || strings.Contains(lower, "operation not permitted") {
					result.ToolDiagnostic = "sandbox_unavailable"
				}
			}
		case "turn.completed":
			if (!started || completed) && !failed {
				failed = true
				result.Diagnostic = "invalid_event_stream"
			}
			completed = true
		case "turn.failed", "error":
			if !failed {
				result.Diagnostic = classify(event.Message + " " + event.Error.Message)
			}
			failed = true
		}
	}
	if scanner.Err() != nil {
		_, _ = io.Copy(io.Discard, r)
		result.Diagnostic = "invalid_event_stream"
		failed = true
	}
	if !started && !failed {
		result.Diagnostic = "missing_session"
	}
	if !started || !completed || failed {
		return result, errors.New(result.Diagnostic)
	}
	result.Status = "completed"
	result.Diagnostic = "completed"
	return result, nil
}
