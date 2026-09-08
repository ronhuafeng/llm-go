package codexsdk

import (
	appserverv2 "github.com/ronhuafeng/llm-go/codexsdk/internal/protocolschema/appserver/v2"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

// RuntimeCompatibilityUnknown is the only compatibility kind the current
// initialize Server Observation can support. userAgent and platform fields
// are identity, not whole-surface compatibility.
const RuntimeCompatibilityUnknown RuntimeCompatibilityKind = "unknown"

// GeneratedBaselineProvenance is Generated Baseline Provenance: the
// checked-in upstream Codex protocol baseline from which this module's
// generated surface was built. Values come from baseline_metadata.json.
type GeneratedBaselineProvenance struct {
	SourceRepo    string
	SourceRefKind string
	SourceRefName string
	SourceCommit  string
}

// RuntimeAppServerObservation is a Runtime App-Server Observation:
// identity the connected app-server reported through initialize. Observed
// is false when no initialize Server Observation has been established.
// Empty reported fields stay empty; they are not inferred from Command,
// paths, requested values, or CI package selection.
type RuntimeAppServerObservation struct {
	Observed       bool
	UserAgent      string
	CodexHome      string
	PlatformFamily string
	PlatformOs     string
}

// RuntimeCompatibilityKind is a protocol-evidenced compatibility claim.
type RuntimeCompatibilityKind string

// RuntimeCompatibility is Runtime Compatibility: the relationship between
// the connected app-server and the generated baseline. Current initialize
// reports no usable compatibility fact, so the kind is unknown even when
// userAgent matches a generator label.
type RuntimeCompatibility struct {
	Kind RuntimeCompatibilityKind
}

// ConnectionProvenance is Connection Provenance: Generated Baseline
// Provenance plus this connection's Runtime App-Server Observation and
// Runtime Compatibility. The facts stay separate; a successful request
// does not rewrite them.
type ConnectionProvenance struct {
	GeneratedBaseline GeneratedBaselineProvenance
	RuntimeAppServer  RuntimeAppServerObservation
	Compatibility     RuntimeCompatibility
}

// Unknown reports that no protocol evidence supports a compatibility claim.
func (c RuntimeCompatibility) Unknown() bool {
	return c.Kind == "" || c.Kind == RuntimeCompatibilityUnknown
}

// GeneratedBaseline returns the checked-in generated baseline provenance.
func GeneratedBaseline() GeneratedBaselineProvenance {
	metadata, err := appserverv2.LoadBaselineMetadata()
	if err != nil {
		panic("codexsdk: checked-in baseline metadata is unreadable: " + err.Error())
	}
	return GeneratedBaselineProvenance{
		SourceRepo:    metadata.SourceRepo,
		SourceRefKind: metadata.SourceRefKind,
		SourceRefName: metadata.SourceRefName,
		SourceCommit:  metadata.SourceCommit,
	}
}

// ObserveRuntimeAppServer preserves an initialize Server Observation as
// runtime identity. It does not judge compatibility.
func ObserveRuntimeAppServer(response protocolv2.InitializeResponse) RuntimeAppServerObservation {
	return RuntimeAppServerObservation{
		Observed:       true,
		UserAgent:      response.UserAgent,
		CodexHome:      response.CodexHome,
		PlatformFamily: response.PlatformFamily,
		PlatformOs:     response.PlatformOs,
	}
}

// RuntimeCompatibilityOf returns the compatibility claim supported by an
// initialize Server Observation. Current protocol fields cannot prove the
// generated surface compatible or incompatible with the connected app-server.
func RuntimeCompatibilityOf(response protocolv2.InitializeResponse) RuntimeCompatibility {
	_ = response
	return RuntimeCompatibility{Kind: RuntimeCompatibilityUnknown}
}

// Provenance returns checked-in generated baseline identity plus this
// connection's initialize observation. Compatibility stays unknown unless
// the protocol reports a usable compatibility fact.
func (c *Client) Provenance() ConnectionProvenance {
	compatibility := RuntimeCompatibility{Kind: RuntimeCompatibilityUnknown}
	var runtime RuntimeAppServerObservation
	if c != nil {
		runtime = c.runtimeAppServer
		if c.runtimeCompatibility.Kind != "" {
			compatibility = c.runtimeCompatibility
		}
	}
	return ConnectionProvenance{
		GeneratedBaseline: GeneratedBaseline(),
		RuntimeAppServer:  runtime,
		Compatibility:     compatibility,
	}
}
