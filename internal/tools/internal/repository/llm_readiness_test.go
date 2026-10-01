package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLLMReadinessHTTPResults(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	script := workflowRunScript(t, readWorkflow(t, root, "llm-readiness.yml"))
	const completed = "data: {\"type\":\"response.completed\"}\n\n"
	const privateBody = "private-response-fixture"
	const key = "readiness-fixture-key"
	for _, test := range []struct {
		name, body, suffix, message string
		status                      int
		failure, disconnected       bool
	}{
		{name: "base URL", status: 200, body: completed, message: "ready"},
		{name: "full endpoint", status: 200, body: completed, suffix: "/responses", message: "ready"},
		{name: "CRLF events and trailing slash", status: 200, body: strings.ReplaceAll(completed, "\n", "\r\n"), suffix: "/responses/", message: "ready"},
		{name: "unauthorized", status: 401, body: privateBody, failure: true, message: "HTTP 401"},
		{name: "missing completion", status: 200, body: "data: {\"type\":\"response.created\"}\n\n", failure: true, message: "completion event missing"},
		{name: "failed", status: 200, body: "data: {\"type\":\"response.failed\"}\n\n" + completed, failure: true, message: "failed response event"},
		{name: "incomplete", status: 200, body: "data: {\"type\":\"response.incomplete\"}\n\n" + completed, failure: true, message: "failed response event"},
		{name: "error", status: 200, body: "data: {\"type\":\"error\"}\n\n" + completed, failure: true, message: "failed response event"},
		{name: "transport", failure: true, disconnected: true, message: "transport failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("probe did not send the intended authenticated Responses request")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("invalid probe payload: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				reasoning, _ := payload["reasoning"].(map[string]any)
				if payload["model"] != "gpt-6-sol" || reasoning["effort"] != "high" || payload["stream"] != true || payload["store"] != false {
					t.Error("probe request did not use the canonical streaming model configuration")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			if test.disconnected {
				server.Close()
			}
			temporary := t.TempDir()
			baseURL := server.URL + "/v1" + test.suffix
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", script)
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"), "TMPDIR=" + temporary,
				"LLM_API_KEY=" + key, "LLM_MODEL=gpt-6-sol", "LLM_BASE_URL= \t" + baseURL + " \t",
			}
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err != nil) != test.failure || !strings.Contains(string(output), test.message) {
				t.Fatalf("failure=%v, err=%v, output=%s", test.failure, err, output)
			}
			for _, sensitive := range []string{key, baseURL, privateBody} {
				if strings.Contains(string(output), sensitive) {
					t.Error("probe logged sensitive fixture data")
				}
			}
			wantRequests := int32(1)
			if test.disconnected {
				wantRequests = 0
			}
			if requests.Load() != wantRequests {
				t.Errorf("requests=%d, want %d", requests.Load(), wantRequests)
			}
			files, err := os.ReadDir(temporary)
			if err != nil || len(files) != 0 {
				t.Fatalf("probe did not remove temporary response files: %v", err)
			}
		})
	}
}
