package livecodex

import (
	"bytes"
	"strings"
	"testing"
)

func TestResultsRequireExecutedUnskippedSuite(t *testing.T) {
	for _, test := range []struct {
		name, events string
		valid        bool
	}{
		{"success", `{"Action":"run","Test":"TestLiveCodexSmoke"}
{"Action":"pass","Test":"TestLiveCodexSmoke"}`, true},
		{"zero tests", `{"Action":"pass"}`, false},
		{"package failure", `{"Action":"run","Test":"TestLiveCodexSmoke"}
{"Action":"pass","Test":"TestLiveCodexSmoke"}
{"Action":"fail"}`, false},
		{"skip", `{"Action":"run","Test":"TestLiveCodexSmoke"}
{"Action":"skip","Test":"TestLiveCodexSmoke"}`, false},
		{"failed", `{"Action":"run","Test":"TestLiveCodexSmoke"}
{"Action":"fail","Test":"TestLiveCodexSmoke"}`, false},
		{"incomplete", `{"Action":"run","Test":"TestLiveCodexSmoke"}`, false},
		{"child skipped", `{"Action":"run","Test":"TestLiveCodexSuite"}
{"Action":"run","Test":"TestLiveCodexSuite/child"}
{"Action":"skip","Test":"TestLiveCodexSuite/child"}
{"Action":"pass","Test":"TestLiveCodexSuite"}`, false},
		{"malformed", `{`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			err := CheckResults(strings.NewReader(test.events), &out)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, err=%v", test.valid, err)
			}
		})
	}
}

func TestResultsDoNotForwardPrivateTranscript(t *testing.T) {
	var out bytes.Buffer
	events := `{"Action":"run","Test":"TestLiveCodexSmoke"}
{"Action":"output","Test":"TestLiveCodexSmoke","Output":"private transcript and secret-key"}
{"Action":"pass","Test":"TestLiveCodexSmoke"}`
	if err := CheckResults(strings.NewReader(events), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "private") || strings.Contains(out.String(), "secret-key") {
		t.Fatal("raw test output leaked")
	}
	if !strings.Contains(out.String(), "live_scenario=TestLiveCodexSmoke live_test_result=pass") {
		t.Fatal("scenario result was lost")
	}
}

func TestResultsRetainStoryStagesWithoutPrivateContent(t *testing.T) {
	var out bytes.Buffer
	events := `{"Action":"run","Test":"TestLiveCodexThreadContinuation"}
{"Action":"output","Test":"TestLiveCodexThreadContinuation","Output":"    live_codex_test.go:128: live_story.stage=fork outcome=completed\n    live_codex_test.go:132: live_story.stage=fork-turn outcome=started\n    live_codex_test.go:133: live_story.stage=private-content outcome=completed\n    live_codex_test.go:190: live_failure.stage=cleanup\n    live_private_response_test.go:1: private-response"}
{"Action":"pass","Test":"TestLiveCodexThreadContinuation"}`
	if err := CheckResults(strings.NewReader(events), &out); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"live_story.stage=fork outcome=completed", "live_story.stage=fork-turn outcome=started", "live_failure.stage=cleanup"} {
		if !strings.Contains(out.String(), fact) {
			t.Errorf("missing bounded stage: %s", fact)
		}
	}
	if strings.Contains(out.String(), "private") {
		t.Fatal("private content was forwarded as a story stage")
	}
}
