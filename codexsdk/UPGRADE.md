# Upgrade codexsdk

Current module: `github.com/ronhuafeng/llm-go/codexsdk`.
Install the current tag with `go get github.com/ronhuafeng/llm-go/codexsdk@v0.7.0`.

## From `codexsdk-go` v0.5.1 to `codexsdk` v0.6.0

| Legacy import | Current import |
| --- | --- |
| `github.com/ronhuafeng/codexsdk-go/codexsdk` | `github.com/ronhuafeng/llm-go/codexsdk` |
| `github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2` | `github.com/ronhuafeng/llm-go/codexsdk/protocolv2` |

The main package is flattened to the module root. There is no
`codexsdk/codexsdk` compatibility package. Do not add a `replace` or
forwarding package.

## From v0.6.1 to v0.7.0

v0.7.0 publishes the generated surface from `openai/codex` `rust-v0.150.1`.
Handwritten Lifecycle APIs stay the same except caller-local `Stream.Next`
cancellation.

Removed stable generated facts: `AmazonBedrockCredentialSource` and its
`AwsManaged`/`CodexManaged` values, `AccountAmazonBedrock.CredentialSource`,
`AppMetadata.FirstPartyType`, `HookMetadata.Command`, `HookMetadata.HandlerType`,
`McpToolCallAppContext.TemplateID`.

These types are now mixed (stable members remain; new experimental members
need experimental runtime opt-in): `codexsdk.Accounts`, `codexsdk.MCPServers`,
`codexsdk.Plugins`, `protocolv2.ThreadMetadataUpdateParams`.

`Stream.Next` context cancellation stops only that Exact Run History Cursor.
Call `Close` for Shared Run Cancellation.
