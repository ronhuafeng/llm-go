package codexsdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

var checkedInSourceCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestGeneratedBaselineMatchesCheckedInMetadata(t *testing.T) {
	authority := readCheckedInBaselineMetadata(t)
	got := GeneratedBaseline()
	if got.SourceRepo != authority.SourceRepo {
		t.Errorf("SourceRepo = %q, want checked-in source_repo %q", got.SourceRepo, authority.SourceRepo)
	}
	if got.SourceRefKind != authority.SourceRefKind {
		t.Errorf("SourceRefKind = %q, want checked-in source_ref_kind %q", got.SourceRefKind, authority.SourceRefKind)
	}
	if got.SourceRefName != authority.SourceRefName {
		t.Errorf("SourceRefName = %q, want checked-in source_ref_name %q", got.SourceRefName, authority.SourceRefName)
	}
	if got.SourceCommit != authority.SourceCommit {
		t.Errorf("SourceCommit = %q, want checked-in source_commit %q", got.SourceCommit, authority.SourceCommit)
	}
	if !checkedInSourceCommit.MatchString(got.SourceCommit) {
		t.Errorf("SourceCommit = %q, want 40 lowercase hex characters from baseline metadata", got.SourceCommit)
	}
}

func TestObserveRuntimeAppServerPreservesReportedIdentity(t *testing.T) {
	observation := ObserveRuntimeAppServer(protocolv2.InitializeResponse{
		CodexHome:      "/tmp/codex-home",
		PlatformFamily: "unix",
		PlatformOs:     "linux",
		UserAgent:      "codex-cli 0.153.4",
	})
	if !observation.Observed {
		t.Fatal("reported initialize Server Observation must be Observed")
	}
	if observation.UserAgent != "codex-cli 0.153.4" {
		t.Errorf("UserAgent = %q, want the initialize userAgent", observation.UserAgent)
	}
	if observation.CodexHome != "/tmp/codex-home" || observation.PlatformFamily != "unix" || observation.PlatformOs != "linux" {
		t.Errorf("identity fields = %#v, want the initialize Server Observation", observation)
	}
	if !RuntimeCompatibilityOf(protocolv2.InitializeResponse{UserAgent: "codex-cli 0.153.4"}).Unknown() {
		t.Fatal("a reported userAgent is identity, not whole-surface compatibility")
	}
}

func TestObserveRuntimeAppServerPreservesMismatchedIdentity(t *testing.T) {
	authority := readCheckedInBaselineMetadata(t)
	observation := ObserveRuntimeAppServer(protocolv2.InitializeResponse{
		CodexHome:      "/other/codex-home",
		PlatformFamily: "windows",
		PlatformOs:     "windows",
		UserAgent:      "codex-cli 9.9.9",
	})
	if observation.UserAgent == authority.CodexVersion {
		t.Fatal("test fixture userAgent unexpectedly equals the generator label")
	}
	if observation.UserAgent != "codex-cli 9.9.9" {
		t.Errorf("UserAgent = %q, want the mismatched initialize userAgent", observation.UserAgent)
	}
	baseline := GeneratedBaseline()
	if observation.UserAgent == baseline.SourceRefName || observation.UserAgent == baseline.SourceCommit {
		t.Fatal("runtime identity must stay a separate fact from generated baseline labels")
	}
	if !RuntimeCompatibilityOf(protocolv2.InitializeResponse{UserAgent: "codex-cli 9.9.9"}).Unknown() {
		t.Fatal("identity mismatch is not a fail-closed compatibility judgment")
	}
}

func TestRuntimeCompatibilityRemainsUnknownWithoutProtocolEvidence(t *testing.T) {
	authority := readCheckedInBaselineMetadata(t)
	matching := protocolv2.InitializeResponse{UserAgent: authority.CodexVersion}
	if !RuntimeCompatibilityOf(matching).Unknown() {
		t.Fatalf("userAgent %q matching the generator label must not prove runtime compatibility", authority.CodexVersion)
	}
	empty := protocolv2.InitializeResponse{}
	observation := ObserveRuntimeAppServer(empty)
	if !observation.Observed {
		t.Fatal("an initialize Server Observation is observed even when identity fields are empty")
	}
	if observation.UserAgent != "" {
		t.Errorf("empty initialize userAgent = %q, want empty reported identity", observation.UserAgent)
	}
	if !RuntimeCompatibilityOf(empty).Unknown() {
		t.Fatal("empty initialize identity must leave compatibility unknown")
	}
}

func TestRuntimeFactsAreNotInferredFromCommandOrRequestedModel(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	command := fakeCommand("initialize-user-agent", "observed-app-server")
	root, err := New(ClientOptions{
		CWD:     t.TempDir(),
		Command: command,
		Initialize: protocolv2.InitializeParams{
			ClientInfo: protocolv2.ClientInfo{
				Name:    "consumer",
				Version: "requested-not-a-runtime-fact",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	provenance := root.Provenance()
	runtime := provenance.RuntimeAppServer
	if !runtime.Observed || runtime.UserAgent != "observed-app-server" {
		t.Fatalf("runtime observation = %#v, want initialize userAgent observed-app-server", runtime)
	}
	if runtime.UserAgent == command[0] || runtime.UserAgent == filepath.Base(command[0]) {
		t.Fatal("runtime identity must not be inferred from ClientOptions.Command")
	}
	if !provenance.Compatibility.Unknown() {
		t.Fatal("command path and requested clientInfo must not invent compatibility")
	}
	if provenance.GeneratedBaseline != GeneratedBaseline() {
		t.Fatalf("client generated baseline = %#v, want package authority %#v", provenance.GeneratedBaseline, GeneratedBaseline())
	}
}

func TestSuccessfulTurnDoesNotProveRuntimeCompatibility(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	root, err := New(ClientOptions{CWD: t.TempDir(), Command: fakeCommand("happy")})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	_, err = root.ThreadRunner().Start(context.Background(), StartThreadRunRequest{
		Thread: protocolv2.ThreadStartParams{Model: protocolv2.Value("gpt-exact")},
		Turn:   protocolv2.TurnStartParams{Input: []protocolv2.UserInput{protocolv2.NewUserInputText(protocolv2.UserInputText{Text: "hello"})}},
	})
	if err != nil {
		t.Fatal(err)
	}
	provenance := root.Provenance()
	if !provenance.RuntimeAppServer.Observed || provenance.RuntimeAppServer.UserAgent != "fake-app-server" {
		t.Fatalf("successful turn erased initialize observation: %#v", provenance.RuntimeAppServer)
	}
	if !provenance.Compatibility.Unknown() {
		t.Fatal("one successful turn must not be exposed as whole-surface compatibility")
	}
}

func TestClosedClientRetainsEstablishedProvenance(t *testing.T) {
	t.Setenv("CODEXSDK_FAKE_RECORD", tempRecord(t))
	root, err := New(ClientOptions{CWD: t.TempDir(), Command: fakeCommand("happy")})
	if err != nil {
		t.Fatal(err)
	}
	before := root.Provenance()
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	after := root.Provenance()
	if after.RuntimeAppServer != before.RuntimeAppServer {
		t.Fatalf("close rewrote runtime observation: before %#v after %#v", before.RuntimeAppServer, after.RuntimeAppServer)
	}
	if after.GeneratedBaseline != before.GeneratedBaseline {
		t.Fatal("close must not erase generated baseline provenance")
	}
	if !after.Compatibility.Unknown() {
		t.Fatal("close must not invent compatibility")
	}
}

func TestZeroClientRuntimeObservationIsUnobserved(t *testing.T) {
	var root Client
	provenance := root.Provenance()
	if provenance.RuntimeAppServer.Observed {
		t.Fatal("zero Client has no initialize Server Observation")
	}
	if provenance.GeneratedBaseline != GeneratedBaseline() {
		t.Fatal("generated baseline is checked-in, not connection-dependent")
	}
	if !provenance.Compatibility.Unknown() {
		t.Fatal("unconnected client compatibility must remain unknown")
	}
}

type checkedInBaselineMetadata struct {
	CodexVersion  string `json:"codex_version"`
	SourceCommit  string `json:"source_commit"`
	SourceRefKind string `json:"source_ref_kind"`
	SourceRefName string `json:"source_ref_name"`
	SourceRepo    string `json:"source_repo"`
}

func readCheckedInBaselineMetadata(t *testing.T) checkedInBaselineMetadata {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("internal", "protocolschema", "appserver", "v2", "baseline_metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata checkedInBaselineMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.SourceCommit == "" || metadata.SourceRefName == "" || metadata.SourceRepo == "" {
		t.Fatalf("checked-in baseline metadata missing public provenance: %#v", metadata)
	}
	return metadata
}
