# rust-v0.164.0 full public-target corpus

`rust-v0.164.0-full-target.json.gz` retains public JSON Schema documents and
the production `schemaOraclePublicRequest` selector's complete changed public
bindings for this upgrade. It contains no executable candidate or model output.

- Upstream: `openai/codex`, `rust-v0.164.0`,
  `6aaac694803004a63c6f393d153c96d22521a6b9`.
- Accepted baseline: `62476945688c5b6c4d25daf9614777904d9d14a5`.
- Source generation: native `protocolupgrade sync -offline-upstream`, using
  exact-source `cargo run --locked -p codex-cli` for experimental and stable
  schemas and the same commit's `common.rs` for public root mappings.
- Fixture SHA-256:
  `7763920cbb04fa2f934c95654204cb22eca9855bea9ba49b424c6996ef3c6e0d`.

The unchanged source selector chose 65 targets in 27 owning documents. Generated
Go declarations do not select new targets. Documents retain the generated source
JSON as raw values inside the gzip fixture; selection includes every changed
reachable public contract, including changes inherited through references.

The original fixed-target acceptance failed while building
`v2/ThreadTurnsListResponse.json`: 134,218,053 accounted bytes at probe 132,128
exceeded the existing 128 MiB bound. The accepted oracle at the baseline above
was then executed one target at a time, preserving each target's witness and
mutation construction while avoiding the aggregate limit for measurement.
It produced 139,921 ordered probes with 125,880,455 bytes of raw witness JSON.
No bound was changed in the candidate verifier.

Frozen hashes from that accepted implementation are:

- Ordered schema/input/verdict sequence:
  `47e9e66a3ac207125da8991d6cd96e7c9914f6d2754a59e4d5577c2dedb3f76f`.
- Target acceptance metadata (bindings, fields and canonical omission rules):
  `640a30f807adaa36953ecbdeb6bfb1bd3f9792dd8627ee3877b01f0888a6d27a`.

`TestUpstream164FullTargetCorpusBudgetAndIdentity` exercises aggregate production
Build and Render, decodes the actual generated wire corpus, restores every
input and diagnostic, and checks those original hashes and counts. Identical
immutable JSON strings and wire table entries share representation across
targets; target/probe execution and verdicts remain independent. The 128 MiB
corpus, 8 MiB compressed and 12 MiB generated-source bounds remain unchanged.
The shared wire is 132,179,977 decompressed bytes, 2,376,970 compressed bytes,
and retains 130,353 distinct JSON values for all 139,921 probe obligations.

The full upstream regression runs in the `schemaoracle/fulltarget164` test
package so both full corpora run under the unchanged standard per-package race
timeout. No assertion is skipped or removed.

The earlier rust-v0.162.1 corpus retains its original complete sequence and
metadata hashes too. Focused fixtures separately execute valid evolution and
real defects: invalid references, frozen-input mutation, lost JSON values,
missing branch validation, intersected open branches and undeclared fields
still fail. The SDK image-generation tests compare single, both and mixed-type
branches directly with this upgrade's unmodified source schema.
