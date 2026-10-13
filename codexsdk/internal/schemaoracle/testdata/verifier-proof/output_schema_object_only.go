// Fixture for the accepted v2/TurnStartParams.json and standalone strict record
// contract in docs/protocol-sync.md. Frozen schema below is derived only from
// that immutable source, with the accepted unknown-record-field overlay.
package verifierfixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	native "github.com/google/jsonschema-go/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

type TurnStartParams struct {
	F0  json.RawMessage `json:"additionalContext,omitempty"`
	F1  json.RawMessage `json:"approvalPolicy,omitempty"`
	F2  json.RawMessage `json:"approvalsReviewer,omitempty"`
	F3  json.RawMessage `json:"clientUserMessageId,omitempty"`
	F4  json.RawMessage `json:"collaborationMode,omitempty"`
	F5  json.RawMessage `json:"cwd,omitempty"`
	F6  json.RawMessage `json:"cyberAccessProgram,omitempty"`
	F7  json.RawMessage `json:"disabledPluginIds,omitempty"`
	F8  json.RawMessage `json:"effort,omitempty"`
	F9  json.RawMessage `json:"environments,omitempty"`
	F10 json.RawMessage `json:"input,omitempty"`
	F11 json.RawMessage `json:"model,omitempty"`
	F12 json.RawMessage `json:"multiAgentMode,omitempty"`
	F13 json.RawMessage `json:"outputSchema,omitempty"`
	F14 json.RawMessage `json:"permissions,omitempty"`
	F15 json.RawMessage `json:"personality,omitempty"`
	F16 json.RawMessage `json:"responsesapiClientMetadata,omitempty"`
	F17 json.RawMessage `json:"runtimeWorkspaceRoots,omitempty"`
	F18 json.RawMessage `json:"sandboxPolicy,omitempty"`
	F19 json.RawMessage `json:"serviceTier,omitempty"`
	F20 json.RawMessage `json:"serviceTierForTurn,omitempty"`
	F21 json.RawMessage `json:"summary,omitempty"`
	F22 json.RawMessage `json:"threadId,omitempty"`
	F23 json.RawMessage `json:"toolOutput,omitempty"`
	F24 json.RawMessage `json:"turnTrigger,omitempty"`
}

const frozenSource = `{"$schema":"http://json-schema.org/draft-07/schema#","definitions":{"AbsolutePathBuf":{"description":"A path that is guaranteed to be absolute and normalized (though it is not guaranteed to be canonicalized or exist on the filesystem).\n\nIMPORTANT: When deserializing an \u0060AbsolutePathBuf\u0060, a base path must be set using [AbsolutePathBufGuard::new]. If no base path is set, the deserialization will fail unless the path being deserialized is already absolute.","type":"string"},"AdditionalContextEntry":{"properties":{"kind":{"$ref":"#/definitions/AdditionalContextKind"},"value":{"type":"string"}},"required":["kind","value"],"type":"object","additionalProperties":false},"AdditionalContextKind":{"enum":["untrusted","application"],"type":"string"},"ApprovalsReviewer":{"description":"Configures who approval requests are routed to for review. Examples include sandbox escapes, blocked network access, MCP approval prompts, and ARC escalations. Defaults to \u0060user\u0060. \u0060auto_review\u0060 uses a carefully prompted subagent to gather relevant context and apply a risk-based decision framework before approving or denying the request. The legacy value \u0060guardian_subagent\u0060 is accepted for compatibility.","enum":["user","auto_review","guardian_subagent"],"type":"string"},"AskForApproval":{"oneOf":[{"enum":["untrusted","on-request","never"],"type":"string"},{"additionalProperties":false,"properties":{"granular":{"properties":{"mcp_elicitations":{"type":"boolean"},"request_permissions":{"default":false,"type":"boolean"},"rules":{"type":"boolean"},"sandbox_approval":{"type":"boolean"},"skill_approval":{"default":false,"type":"boolean"}},"required":["mcp_elicitations","rules","sandbox_approval"],"type":"object","additionalProperties":false}},"required":["granular"],"title":"GranularAskForApproval","type":"object"}]},"ByteRange":{"properties":{"end":{"format":"uint","minimum":0.0,"type":"integer"},"start":{"format":"uint","minimum":0.0,"type":"integer"}},"required":["end","start"],"type":"object","additionalProperties":false},"CollaborationMode":{"description":"Collaboration mode for a Codex session.","properties":{"mode":{"$ref":"#/definitions/ModeKind"},"settings":{"$ref":"#/definitions/Settings"}},"required":["mode","settings"],"type":"object","additionalProperties":false},"CyberAccessProgram":{"description":"Requested cyber treatment for a ChatGPT-authenticated Codex turn. Authorization and model-tier restrictions remain server-owned.","enum":["standard","daybreakBlue","daybreakRed"],"type":"string"},"FunctionCallOutputBody":{"anyOf":[{"type":"string"},{"items":{"$ref":"#/definitions/FunctionCallOutputContentItem"},"type":"array"}]},"FunctionCallOutputContentItem":{"description":"Responses API compatible content items that can be returned by a tool call. This is a subset of ContentItem with the types we support as function call outputs.","oneOf":[{"properties":{"text":{"type":"string"},"type":{"enum":["input_text"],"title":"InputTextFunctionCallOutputContentItemType","type":"string"}},"required":["text","type"],"title":"InputTextFunctionCallOutputContentItem","type":"object","additionalProperties":false},{"title":"InputImageFunctionCallOutputContentItem","anyOf":[{"properties":{"detail":{"anyOf":[{"$ref":"#/definitions/ImageDetail"},{"type":"null"}]},"type":{"enum":["input_image"],"title":"InputImageFunctionCallOutputContentItemType","type":"string"},"image_url":{"type":"string"},"file_id":{}},"required":["type","image_url"],"title":"ImageUrlFunctionCallOutputContentItem","type":"object","additionalProperties":false},{"properties":{"detail":{"anyOf":[{"$ref":"#/definitions/ImageDetail"},{"type":"null"}]},"type":{"enum":["input_image"],"title":"InputImageFunctionCallOutputContentItemType","type":"string"},"file_id":{"type":"string"},"image_url":{}},"required":["type","file_id"],"title":"FileIdFunctionCallOutputContentItem","type":"object","additionalProperties":false}]},{"properties":{"audio_url":{"type":"string"},"type":{"enum":["input_audio"],"title":"InputAudioFunctionCallOutputContentItemType","type":"string"}},"required":["audio_url","type"],"title":"InputAudioFunctionCallOutputContentItem","type":"object","additionalProperties":false},{"properties":{"encrypted_content":{"type":"string"},"type":{"enum":["encrypted_content"],"title":"EncryptedContentFunctionCallOutputContentItemType","type":"string"}},"required":["encrypted_content","type"],"title":"EncryptedContentFunctionCallOutputContentItem","type":"object","additionalProperties":false}]},"ImageDetail":{"enum":["auto","low","high","original"],"type":"string"},"LegacyAppPathString":{"type":"string"},"ModeKind":{"description":"Initial collaboration mode to use when the TUI starts.","enum":["plan","default"],"type":"string"},"MultiAgentMode":{"description":"Controls the effective multi-agent delegation instructions for a turn. \u0060custom\u0060 means the configured mode hint defines the policy instead of a built-in policy.","oneOf":[{"enum":["explicitRequestOnly","proactive"],"type":"string"},{"additionalProperties":false,"properties":{"custom":{"type":"string"}},"required":["custom"],"title":"CustomMultiAgentMode","type":"object"}]},"NetworkAccess":{"enum":["restricted","enabled"],"type":"string"},"Personality":{"description":"Deprecated: \u0060friendly\u0060 and \u0060pragmatic\u0060 no longer select a style.","enum":["none","friendly","pragmatic"],"type":"string"},"ReasoningEffort":{"description":"A non-empty reasoning effort value advertised by the model.","minLength":1,"type":"string"},"ReasoningSummary":{"description":"A summary of the reasoning performed by the model. This can be useful for debugging and understanding the model's reasoning process. See https://platform.openai.com/docs/guides/reasoning?api-mode=responses#reasoning-summaries","oneOf":[{"enum":["auto","concise","detailed"],"type":"string"},{"description":"Option to disable reasoning summaries.","enum":["none"],"type":"string"}]},"SandboxPolicy":{"oneOf":[{"properties":{"type":{"enum":["dangerFullAccess"],"title":"DangerFullAccessSandboxPolicyType","type":"string"}},"required":["type"],"title":"DangerFullAccessSandboxPolicy","type":"object","additionalProperties":false},{"properties":{"networkAccess":{"default":false,"type":"boolean"},"type":{"enum":["readOnly"],"title":"ReadOnlySandboxPolicyType","type":"string"}},"required":["type"],"title":"ReadOnlySandboxPolicy","type":"object","additionalProperties":false},{"properties":{"networkAccess":{"allOf":[{"$ref":"#/definitions/NetworkAccess"}],"default":"restricted"},"type":{"enum":["externalSandbox"],"title":"ExternalSandboxSandboxPolicyType","type":"string"}},"required":["type"],"title":"ExternalSandboxSandboxPolicy","type":"object","additionalProperties":false},{"properties":{"excludeSlashTmp":{"default":false,"type":"boolean"},"excludeTmpdirEnvVar":{"default":false,"type":"boolean"},"networkAccess":{"default":false,"type":"boolean"},"type":{"enum":["workspaceWrite"],"title":"WorkspaceWriteSandboxPolicyType","type":"string"},"writableRoots":{"default":[],"items":{"$ref":"#/definitions/AbsolutePathBuf"},"type":"array"}},"required":["type"],"title":"WorkspaceWriteSandboxPolicy","type":"object","additionalProperties":false}]},"Settings":{"description":"Settings for a collaboration mode.","properties":{"developer_instructions":{"type":["string","null"]},"model":{"type":"string"},"reasoning_effort":{"anyOf":[{"$ref":"#/definitions/ReasoningEffort"},{"type":"null"}]}},"required":["model"],"type":"object","additionalProperties":false},"TextElement":{"properties":{"byteRange":{"allOf":[{"$ref":"#/definitions/ByteRange"}],"description":"Byte range in the parent \u0060text\u0060 buffer that this element occupies."},"placeholder":{"description":"Optional human-readable placeholder for the element, displayed in the UI.","type":["string","null"]}},"required":["byteRange"],"type":"object","additionalProperties":false},"TurnEnvironmentParams":{"properties":{"cwd":{"$ref":"#/definitions/LegacyAppPathString"},"environmentId":{"type":"string"},"runtimeWorkspaceRoots":{"description":"Environment-native runtime workspace roots. Omitted defaults to \u0060cwd\u0060.","items":{"$ref":"#/definitions/LegacyAppPathString"},"type":["array","null"]}},"required":["cwd","environmentId"],"type":"object","additionalProperties":false},"TurnToolOutput":{"properties":{"name":{"type":"string"},"namespace":{"type":["string","null"]},"output":{"$ref":"#/definitions/FunctionCallOutputBody"}},"required":["name","output"],"type":"object","additionalProperties":false},"UserInput":{"oneOf":[{"properties":{"text":{"type":"string"},"text_elements":{"default":[],"description":"UI-defined spans within \u0060text\u0060 used to render or persist special elements.","items":{"$ref":"#/definitions/TextElement"},"type":"array"},"type":{"enum":["text"],"title":"TextUserInputType","type":"string"}},"required":["text","type"],"title":"TextUserInput","type":"object","additionalProperties":false},{"title":"ImageUserInput","anyOf":[{"properties":{"detail":{"anyOf":[{"$ref":"#/definitions/ImageDetail"},{"type":"null"}],"default":null},"type":{"enum":["image"],"title":"ImageUserInputType","type":"string"},"url":{"type":"string"},"fileId":{}},"required":["type","url"],"title":"UrlUserInput","type":"object","additionalProperties":false},{"properties":{"detail":{"anyOf":[{"$ref":"#/definitions/ImageDetail"},{"type":"null"}],"default":null},"type":{"enum":["image"],"title":"ImageUserInputType","type":"string"},"fileId":{"type":"string"},"url":{}},"required":["type","fileId"],"title":"FileIdUserInput","type":"object","additionalProperties":false}]},{"properties":{"detail":{"anyOf":[{"$ref":"#/definitions/ImageDetail"},{"type":"null"}],"default":null},"path":{"type":"string"},"type":{"enum":["localImage"],"title":"LocalImageUserInputType","type":"string"}},"required":["path","type"],"title":"LocalImageUserInput","type":"object","additionalProperties":false},{"properties":{"type":{"enum":["audio"],"title":"AudioUserInputType","type":"string"},"url":{"type":"string"}},"required":["type","url"],"title":"AudioUserInput","type":"object","additionalProperties":false},{"properties":{"path":{"type":"string"},"type":{"enum":["localAudio"],"title":"LocalAudioUserInputType","type":"string"}},"required":["path","type"],"title":"LocalAudioUserInput","type":"object","additionalProperties":false},{"properties":{"name":{"type":"string"},"path":{"type":"string"},"type":{"enum":["skill"],"title":"SkillUserInputType","type":"string"}},"required":["name","path","type"],"title":"SkillUserInput","type":"object","additionalProperties":false},{"properties":{"name":{"type":"string"},"path":{"type":"string"},"type":{"enum":["mention"],"title":"MentionUserInputType","type":"string"}},"required":["name","path","type"],"title":"MentionUserInput","type":"object","additionalProperties":false}]}},"properties":{"additionalContext":{"additionalProperties":{"$ref":"#/definitions/AdditionalContextEntry"},"description":"Optional client-provided context fragments keyed by an opaque source identifier.","type":["object","null"]},"approvalPolicy":{"anyOf":[{"$ref":"#/definitions/AskForApproval"},{"type":"null"}],"description":"Override the approval policy for this turn and subsequent turns."},"approvalsReviewer":{"anyOf":[{"$ref":"#/definitions/ApprovalsReviewer"},{"type":"null"}],"description":"Override where approval requests are routed for review on this turn and subsequent turns."},"clientUserMessageId":{"type":["string","null"]},"collaborationMode":{"anyOf":[{"$ref":"#/definitions/CollaborationMode"},{"type":"null"}],"description":"EXPERIMENTAL - Set a pre-set collaboration mode. Takes precedence over model, reasoning_effort, and developer instructions if set.\n\nFor \u0060collaboration_mode.settings.developer_instructions\u0060, \u0060null\u0060 means \"use the built-in instructions for the selected mode\"."},"cwd":{"description":"Override the working directory for this turn and subsequent turns.","type":["string","null"]},"cyberAccessProgram":{"anyOf":[{"$ref":"#/definitions/CyberAccessProgram"},{"type":"null"}],"description":"EXPERIMENTAL - Request a workspace-authorized cyber program for this turn. Omission preserves automatic behavior. This does not grant access."},"disabledPluginIds":{"description":"Replace this thread's disabled plugin IDs. Omitted/null preserves the list; [] clears it.","items":{"type":"string"},"type":["array","null"]},"effort":{"anyOf":[{"$ref":"#/definitions/ReasoningEffort"},{"type":"null"}],"description":"Override the reasoning effort for this turn and subsequent turns."},"environments":{"description":"Optional environments for this turn and subsequent turns.\n\nOmitted uses the thread sticky environments. Empty disables environment access for this turn. Non-empty selects the first environment as the current turn environment for this turn.","items":{"$ref":"#/definitions/TurnEnvironmentParams"},"type":["array","null"]},"input":{"items":{"$ref":"#/definitions/UserInput"},"type":"array"},"model":{"description":"Override the model for this turn and subsequent turns.","type":["string","null"]},"multiAgentMode":{"anyOf":[{"$ref":"#/definitions/MultiAgentMode"},{"type":"null"}],"description":"@deprecated Ignored. Use \u0060effort: \"ultra\"\u0060 for proactive multi-agent behavior."},"outputSchema":{"description":"Optional JSON Schema used to constrain the final assistant message for this turn."},"permissions":{"description":"Select a named permissions profile id for this turn and subsequent turns. Cannot be combined with \u0060sandboxPolicy\u0060.","type":["string","null"]},"personality":{"anyOf":[{"$ref":"#/definitions/Personality"},{"type":"null"}],"description":"@deprecated \u0060friendly\u0060 and \u0060pragmatic\u0060 no longer select a style. Changing this does not rewrite the thread's existing instructions."},"responsesapiClientMetadata":{"additionalProperties":{"type":"string"},"description":"Optional metadata to enrich Codex's ResponsesAPI turn metadata.\n\nEntries are flattened into the JSON string sent as \u0060client_metadata[\"x-codex-turn-metadata\"]\u0060 on ResponsesAPI HTTP and websocket requests.\n\nThey are not sent as top-level ResponsesAPI \u0060client_metadata\u0060 keys, and reserved keys such as \u0060session_id\u0060, \u0060thread_id\u0060, \u0060turn_id\u0060, and \u0060window_id\u0060 cannot be overridden.","type":["object","null"]},"runtimeWorkspaceRoots":{"description":"Replace the thread's runtime workspace roots for this turn and subsequent turns. Paths must be absolute.","items":{"$ref":"#/definitions/AbsolutePathBuf"},"type":["array","null"]},"sandboxPolicy":{"anyOf":[{"$ref":"#/definitions/SandboxPolicy"},{"type":"null"}],"description":"Override the sandbox policy for this turn and subsequent turns."},"serviceTier":{"description":"Override the service tier for this turn and subsequent turns.","type":["string","null"]},"serviceTierForTurn":{"description":"Override the service tier only when this request starts a new turn. Use \"default\" for standard speed. Omitted or null inherits the thread's tier. Does not change the thread's tier or a turn being steered.","type":["string","null"]},"summary":{"anyOf":[{"$ref":"#/definitions/ReasoningSummary"},{"type":"null"}],"description":"Override the reasoning summary for this turn and subsequent turns."},"threadId":{"type":"string"},"toolOutput":{"anyOf":[{"$ref":"#/definitions/TurnToolOutput"},{"type":"null"}]},"turnTrigger":{"description":"Optional source classification for the caller that starts this turn. Ignored when this request steers an already-active turn.","type":["string","null"]}},"required":["input","threadId"],"title":"TurnStartParams","type":"object","additionalProperties":false}`

var contract = func() *validator.Schema {
	var doc any
	d := json.NewDecoder(bytes.NewBufferString(frozenSource))
	d.UseNumber()
	if err := d.Decode(&doc); err != nil {
		panic(err)
	}
	c := validator.NewCompiler()
	if err := c.AddResource("https://fixture.invalid/turn.json", doc); err != nil {
		panic(err)
	}
	s, err := c.Compile("https://fixture.invalid/turn.json")
	if err != nil {
		panic(err)
	}
	return s
}()

func (p *TurnStartParams) UnmarshalJSON(raw []byte) error {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return err
	}
	if err := contract.Validate(value); err != nil {
		return err
	}
	object := value.(map[string]any)
	if document, exists := object["outputSchema"]; exists {
		if err := validateDocument(document); err != nil {
			return err
		}
	}
	type plain TurnStartParams
	return json.Unmarshal(raw, (*plain)(p))
}
func validateDocument(document any) error {
	if _, ok := document.(bool); ok {
		return fmt.Errorf("boolean schemas rejected")
	}
	if _, ok := document.(map[string]any); !ok {
		return fmt.Errorf("invalid schema root")
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	var schema native.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	_, err = schema.Resolve(nil)
	return err
}
