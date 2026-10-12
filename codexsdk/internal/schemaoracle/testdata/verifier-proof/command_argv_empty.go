// Frozen CommandExecParams schema: rust-v0.160.0 and rust-v0.162.1 have
// identical source bytes. Standalone closure follows docs/protocol-sync.md;
// non-empty argv follows protocolv2/protocol_types_test.go:3680-3701,3749-3762.
package verifierfixture

import (
	"bytes"
	"encoding/json"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

type CommandExecParams struct {
	F0  json.RawMessage `json:"command,omitempty"`
	F1  json.RawMessage `json:"cwd,omitempty"`
	F2  json.RawMessage `json:"disableOutputCap,omitempty"`
	F3  json.RawMessage `json:"disableTimeout,omitempty"`
	F4  json.RawMessage `json:"env,omitempty"`
	F5  json.RawMessage `json:"outputBytesCap,omitempty"`
	F6  json.RawMessage `json:"permissionProfile,omitempty"`
	F7  json.RawMessage `json:"processId,omitempty"`
	F8  json.RawMessage `json:"sandboxPolicy,omitempty"`
	F9  json.RawMessage `json:"size,omitempty"`
	F10 json.RawMessage `json:"streamStdin,omitempty"`
	F11 json.RawMessage `json:"streamStdoutStderr,omitempty"`
	F12 json.RawMessage `json:"timeoutMs,omitempty"`
	F13 json.RawMessage `json:"tty,omitempty"`
}

const commandSource = `{"$schema":"http://json-schema.org/draft-07/schema#","definitions":{"AbsolutePathBuf":{"description":"A path that is guaranteed to be absolute and normalized (though it is not guaranteed to be canonicalized or exist on the filesystem).\n\nIMPORTANT: When deserializing an \u0060AbsolutePathBuf\u0060, a base path must be set using [AbsolutePathBufGuard::new]. If no base path is set, the deserialization will fail unless the path being deserialized is already absolute.","type":"string"},"CommandExecTerminalSize":{"description":"PTY size in character cells for \u0060command/exec\u0060 PTY sessions.","properties":{"cols":{"description":"Terminal width in character cells.","format":"uint16","minimum":0.0,"type":"integer"},"rows":{"description":"Terminal height in character cells.","format":"uint16","minimum":0.0,"type":"integer"}},"required":["cols","rows"],"type":"object","additionalProperties":false},"NetworkAccess":{"enum":["restricted","enabled"],"type":"string"},"SandboxPolicy":{"oneOf":[{"properties":{"type":{"enum":["dangerFullAccess"],"title":"DangerFullAccessSandboxPolicyType","type":"string"}},"required":["type"],"title":"DangerFullAccessSandboxPolicy","type":"object","additionalProperties":false},{"properties":{"networkAccess":{"default":false,"type":"boolean"},"type":{"enum":["readOnly"],"title":"ReadOnlySandboxPolicyType","type":"string"}},"required":["type"],"title":"ReadOnlySandboxPolicy","type":"object","additionalProperties":false},{"properties":{"networkAccess":{"allOf":[{"$ref":"#/definitions/NetworkAccess"}],"default":"restricted"},"type":{"enum":["externalSandbox"],"title":"ExternalSandboxSandboxPolicyType","type":"string"}},"required":["type"],"title":"ExternalSandboxSandboxPolicy","type":"object","additionalProperties":false},{"properties":{"excludeSlashTmp":{"default":false,"type":"boolean"},"excludeTmpdirEnvVar":{"default":false,"type":"boolean"},"networkAccess":{"default":false,"type":"boolean"},"type":{"enum":["workspaceWrite"],"title":"WorkspaceWriteSandboxPolicyType","type":"string"},"writableRoots":{"default":[],"items":{"$ref":"#/definitions/AbsolutePathBuf"},"type":"array"}},"required":["type"],"title":"WorkspaceWriteSandboxPolicy","type":"object","additionalProperties":false}]}},"description":"Run a standalone command (argv vector) in the server sandbox without creating a thread or turn.\n\nThe final \u0060command/exec\u0060 response is deferred until the process exits and is sent only after all \u0060command/exec/outputDelta\u0060 notifications for that connection have been emitted.","properties":{"command":{"description":"Command argv vector. Empty arrays are rejected.","items":{"type":"string"},"type":"array"},"cwd":{"description":"Optional working directory. Defaults to the server cwd.","type":["string","null"]},"disableOutputCap":{"description":"Disable stdout/stderr capture truncation for this request.\n\nCannot be combined with \u0060outputBytesCap\u0060.","type":"boolean"},"disableTimeout":{"description":"Disable the timeout entirely for this request.\n\nCannot be combined with \u0060timeoutMs\u0060.","type":"boolean"},"env":{"additionalProperties":{"type":["string","null"]},"description":"Optional environment overrides merged into the server-computed environment.\n\nMatching names override inherited values. Set a key to \u0060null\u0060 to unset an inherited variable.","type":["object","null"]},"outputBytesCap":{"description":"Optional per-stream stdout/stderr capture cap in bytes.\n\nWhen omitted, the server default applies. Cannot be combined with \u0060disableOutputCap\u0060.","format":"uint","minimum":0.0,"type":["integer","null"]},"permissionProfile":{"description":"Optional active permissions profile id for this command.\n\nDefaults to the user's configured permissions when omitted. Cannot be combined with \u0060sandboxPolicy\u0060.","type":["string","null"]},"processId":{"description":"Optional client-supplied, connection-scoped process id.\n\nRequired for \u0060tty\u0060, \u0060streamStdin\u0060, \u0060streamStdoutStderr\u0060, and follow-up \u0060command/exec/write\u0060, \u0060command/exec/resize\u0060, and \u0060command/exec/terminate\u0060 calls. When omitted, buffered execution gets an internal id that is not exposed to the client.","type":["string","null"]},"sandboxPolicy":{"anyOf":[{"$ref":"#/definitions/SandboxPolicy"},{"type":"null"}],"description":"Optional sandbox policy for this command.\n\nUses the same shape as thread/turn execution sandbox configuration and defaults to the user's configured policy when omitted. Cannot be combined with \u0060permissionProfile\u0060."},"size":{"anyOf":[{"$ref":"#/definitions/CommandExecTerminalSize"},{"type":"null"}],"description":"Optional initial PTY size in character cells. Only valid when \u0060tty\u0060 is true."},"streamStdin":{"description":"Allow follow-up \u0060command/exec/write\u0060 requests to write stdin bytes.\n\nRequires a client-supplied \u0060processId\u0060.","type":"boolean"},"streamStdoutStderr":{"description":"Stream stdout/stderr via \u0060command/exec/outputDelta\u0060 notifications.\n\nStreamed bytes are not duplicated into the final response and require a client-supplied \u0060processId\u0060.","type":"boolean"},"timeoutMs":{"description":"Optional timeout in milliseconds.\n\nWhen omitted, the server default applies. Cannot be combined with \u0060disableTimeout\u0060.","format":"int64","type":["integer","null"]},"tty":{"description":"Enable PTY mode.\n\nThis implies \u0060streamStdin\u0060 and \u0060streamStdoutStderr\u0060.","type":"boolean"}},"required":["command"],"title":"CommandExecParams","type":"object","additionalProperties":false}`

var commandContract = func() *validator.Schema {
	var doc any
	d := json.NewDecoder(bytes.NewBufferString(commandSource))
	d.UseNumber()
	if err := d.Decode(&doc); err != nil {
		panic(err)
	}
	c := validator.NewCompiler()
	if err := c.AddResource("https://fixture.invalid/command.json", doc); err != nil {
		panic(err)
	}
	s, err := c.Compile("https://fixture.invalid/command.json")
	if err != nil {
		panic(err)
	}
	return s
}()

func (p *CommandExecParams) UnmarshalJSON(raw []byte) error {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return err
	}
	if err := commandContract.Validate(value); err != nil {
		return err
	}

	type plain CommandExecParams
	return json.Unmarshal(raw, (*plain)(p))
}
