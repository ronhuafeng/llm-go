# Security Policy

`codexsdk-go` is experimental and currently pre-1.0. Security fixes are handled
on the supported default branch and should be released as soon as practical.

## Reporting a Vulnerability

Please do not publish exploitable details in a public issue.

Use GitHub private vulnerability reporting:
https://github.com/ronhuafeng/codexsdk-go/security/advisories/new

If private reporting is not available, open a minimal public issue asking only
for a private channel. Do not include sensitive details, proofs of exploit,
tokens, private paths, transcripts, or account data in that public issue.

Useful reports include:

- affected commit or version;
- operating system and Go version;
- whether a real `codex app-server` was involved;
- minimal reproduction steps;
- expected and observed impact;
- whether credentials, files, or external commands were exposed.

## Scope

In scope:

- SDK transport, JSON-RPC handling, stream handling, and typed protocol
  encoding/decoding bugs that can expose data, hang callers, or mishandle
  approval/server requests;
- generated schema/protocol mismatches that can cause unsafe behavior;
- checked-in secrets, private paths, or private business data.

Out of scope for this repository:

- vulnerabilities in OpenAI services or the official Codex CLI;
- model output quality or prompt-injection behavior unrelated to SDK handling;
- reports requiring access to private user accounts without permission.

## Handling Secrets

Never include API keys, ChatGPT tokens, account IDs, private prompts, private
workspace paths, or command transcripts in public issues, tests, examples,
schema metadata, or generated artifacts.
