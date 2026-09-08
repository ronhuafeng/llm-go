package codexsdk_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestLastAgentMessageSkipsEmptyItems(t *testing.T) {
	items := []protocolv2.ThreadItem{
		protocolv2.NewThreadItemAgentMessage(protocolv2.ThreadItemAgentMessage{ID: "1", Text: ""}),
		protocolv2.NewThreadItemAgentMessage(protocolv2.ThreadItemAgentMessage{ID: "2", Text: " visible "}),
	}
	if got := lastAgentMessage(items); got != "visible" {
		t.Fatalf("lastAgentMessage = %q", got)
	}
}

func TestCompletedWithoutFinalAnswerIsNotARetrySignal(t *testing.T) {
	err := errors.New("codexsdk: turn completed without final_answer agent message")
	if !completedWithoutFinalAnswer(err, string(protocolv2.TurnStatusCompleted)) {
		t.Fatal("completed Grok Turn without phase should remain a completed Turn")
	}
	if completedWithoutFinalAnswer(err, string(protocolv2.TurnStatusFailed)) {
		t.Fatal("failed Turn must not be classified as completed")
	}
}

func TestNonceOfStripsCodeFence(t *testing.T) {
	got := nonceOf("```\n01234567-89ab-cdef-0123-456789abcdef\n```")
	if !uuidShaped.MatchString(got) {
		t.Fatalf("nonce %q is not UUID-shaped", got)
	}
}

func TestDurableEncryptedReasoningIsPresenceOnly(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "09")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout.jsonl")
	line := "{\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"encrypted_content\":\"SECRET\"}}\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if !durableHasEncryptedReasoning(home) {
		t.Fatal("expected encrypted reasoning presence")
	}
}

func TestApplyPatchObservedAcceptsFileChange(t *testing.T) {
	items := []protocolv2.ThreadItem{
		protocolv2.NewThreadItemFileChange(protocolv2.ThreadItemFileChange{
			ID:      "1",
			Status:  protocolv2.PatchApplyStatusCompleted,
			Changes: []protocolv2.FileUpdateChange{},
		}),
	}
	if !applyPatchObserved(items) {
		t.Fatal("completed file_change should prove the apply_patch path")
	}
	if hasCommandExecution(items) {
		t.Fatal("file_change must not count as a shell path")
	}
}

func TestEnsureShellToolDisabledIsIdempotent(t *testing.T) {
	first := ensureShellToolDisabled([]byte("model = \"grok-4.6\"\n"))
	second := ensureShellToolDisabled(first)
	if string(first) != string(second) {
		t.Fatal("disabling shell_tool twice changed the config")
	}
}

func TestScanDurableFactsIgnoresPromptText(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	promptOnly := "{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"use apply_patch and exec_command\"}]}}\n"
	call := "{\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call\",\"name\":\"apply_patch\",\"call_id\":\"c1\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "prompt.jsonl"), []byte(promptOnly), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := scanDurableFacts(home)
	if facts.applyPatchCall || facts.commandExecution {
		t.Fatal("prompt text must not prove a tool path")
	}
	if err := os.WriteFile(filepath.Join(dir, "call.jsonl"), []byte(call), 0o600); err != nil {
		t.Fatal(err)
	}
	facts = scanDurableFacts(home)
	if !facts.applyPatchCall {
		t.Fatal("custom_tool_call named apply_patch should prove the path")
	}
}

func TestScanDurableFactsReadsExtensionImage(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := "{\"type\":\"event_msg\",\"payload\":{\"type\":\"item_completed\",\"item\":{\"type\":\"Extension\",\"kind\":\"image_gen.generation\",\"status\":\"completed\",\"result\":\"cG5n\",\"savedPath\":\"/tmp/a.png\"}}}\n"
	if err := os.WriteFile(filepath.Join(dir, "image.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := scanDurableFacts(home)
	if len(facts.images) != 1 || facts.images[0].SavedPath != "/tmp/a.png" || facts.images[0].Result != "cG5n" {
		t.Fatalf("extension image not parsed: %+v", facts.images)
	}
}
