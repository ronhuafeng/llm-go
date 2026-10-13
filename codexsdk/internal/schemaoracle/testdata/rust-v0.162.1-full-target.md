# Full public-target corpus regression

`rust-v0.162.1-full-target.json.gz` contains only public upstream JSON Schema
source data and the trusted controller's selected schema-to-Go-name bindings.
It contains no candidate Go/Rust executable, proposal patch, or model output.

- Upstream tag: `rust-v0.162.1`
- Upstream commit: `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d`
- Accepted baseline/controller commit: `0ffda123460d96b4e38299343c00f3838f05019f`
- Source checkpoint: formal sync run `38218543560`, archive SHA-256
  `94f5d9873da2d26d5759f5512ad2fce3e23e930843a571521462a895a7fe647a`
- Fixture SHA-256:
  `e10039836585ecf845e33800ad35bb1ec7bbb3dcc7a4ae15ebcaadaa5d9da0ae`

The production `schemaOraclePublicRequest` selector derived 65 changed public
bindings from the accepted schemas, upstream candidate schemas and `common.rs`.
Those bindings use 25 owning JSON documents, retained here with their original
source bytes inside the fixture envelope. Generated Go declarations do not select
reachability or new binding names; they are consulted only to reject surviving
removed source declarations. No candidate proposal was executed to make this
fixture. The selector, accepted schemas and oracle were unchanged between the
baseline above and `2d2c945b0331132003a45bb619c59c1de1a39efe` used for reproduction.

This is the complete selected graph for that drift, not every public schema or
an arbitrary subset. Trusted per-target Build measurements preserve every
obligation, including repeated inputs against different Go bindings:

- 65 targets, 124,376 ordered probes; raw witness JSON: 111,930,719 bytes
- Before representation sharing: diagnostics 18,491,544 bytes and locations
  16,069,244 bytes; old JSON/Valid-only wire 131,780,567 bytes
- Repeated-metadata wire: 169,096,007 bytes (gzip 2,694,106 bytes)
- Reproduced original failure: `v2/ThreadStartResponse.json`, probe count 111,315,
  accounted bytes 134,219,082, over the unchanged 128 MiB corpus bound
- Raw-JSON/diagnostic-table wire: 127,298,903 bytes (gzip 1,991,851 bytes)

`TestFullTargetCorpusBudgetAndIdentity` runs the actual production Build+Render
path, extracts and decodes the generated corpus, and checks all restored input
bytes, verdicts, order, diagnostic text and target metadata. Frozen pre-change
hashes cover both ordered schema/input/verdict records and complete target
acceptance metadata (Go bindings, field checks and parameterless methods).
Neither obligations nor execution are deduplicated; only immutable representation
is shared. The 128 MiB decompressed and 8 MiB compressed limits remain unchanged.

The fixed-target upgrade corrects object-product `anyOf` classification: the
complete record closes its declared field set, while each open alternative
constrains only its own fields. URL/file-ID mixed-type values and values matching
both branches are now covered. The unchanged source fixture produces 65 targets
and 128,968 ordered probes, with a 131,406,260-byte wire corpus (gzip 2,035,302
bytes). Its updated schema/input/verdict sequence hash is
`c74c50d65f5e74a1e17cc448fec276d5465eac447a807c95c4b3a139c16aa389`;
the target acceptance metadata hash is unchanged. Executed evolution and defect
fixtures separately prove that valid mixed types survive and that intersected
branches, missing validation, and dropped fields still fail.

The accepted nonempty `CommandExecParams.command` and opaque experimental
realtime notification item overlays now apply to the validation document only.
The source witness document, all 128,968 inputs, their order, 65 target bindings,
and transport bounds remain intact.
Executable fixtures accept those public contracts and still fail for empty argv,
lost realtime item data, and undeclared outer notification fields.

The unchanged MCP `ResourceContent` source retains its accepted public overlay:
text and blob fields are exclusive, and each resource record is closed. The
frozen baseline MCP assertion remains enforced. This overlay does not close
general anonymous `anyOf` alternatives or the image object product; standalone
fixtures execute both the accepted resource contract and a payload-loss defect.
