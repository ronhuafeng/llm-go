#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/codexsdk_track_upstream.sh --commit <codex-tag-ref-or-commit> [options]

Options:
  --codex-repo <path>   Codex source repo. Defaults to CODEXSDK_CODEX_REPO.
  --codex-bin <path>    Codex binary used to generate schema. Defaults to codex.
  --baseline <path>     Checked-in SDK schema baseline.
  --candidate <path>    Existing generated schema directory for --compare-only.
  --generator <mode>    cargo or binary. Defaults to cargo.
  --out <path>          Output workdir for generated schema and reports.
  --source-ref <name>   Original upstream tag/ref name used for provenance.
  --source-ref-kind <k> Upstream target kind, such as stable_rust_tag.
  --no-fetch            Do not fetch the target commit before resolving it.
  --compare-only        Compare --baseline with --candidate without generating schema.
  --json                Print machine-readable result JSON to stdout.
  --verbose             Print human-readable progress and artifact paths to stderr.

The workflow is read-only for the checked-in baseline. It fetches the requested
Codex tag, ref, or commit when needed, generates an app-server schema snapshot,
and writes review artifacts under --out. In binary mode, --codex-bin must point
to a Codex binary built from the target commit. In cargo mode, the script
creates a temporary worktree at the target commit and runs cargo from that
worktree. In compare-only mode, --commit is recorded as the already-resolved
upstream commit and no Codex repo, worktree, binary, or Cargo command is used.
EOF
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
codex_repo="${CODEXSDK_CODEX_REPO:-}"
codex_bin="codex"
baseline="${repo_root}/codexsdk/internal/protocolschema/appserver/v2"
candidate=""
generator="cargo"
out=""
commit=""
source_ref=""
source_ref_kind=""
fetch=1
worktree=""
compare_only=0
json_output=0
verbose=0

cleanup() {
  if [[ -n "${worktree}" && -d "${worktree}" ]]; then
    git -C "${codex_repo}" worktree remove --force "${worktree}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --codex-repo)
      codex_repo="$2"
      shift 2
      ;;
    --codex-bin)
      codex_bin="$2"
      shift 2
      ;;
    --baseline)
      baseline="$2"
      shift 2
      ;;
    --candidate)
      candidate="$2"
      shift 2
      ;;
    --generator)
      generator="$2"
      shift 2
      ;;
    --out)
      out="$2"
      shift 2
      ;;
    --commit)
      commit="$2"
      shift 2
      ;;
    --source-ref)
      source_ref="$2"
      shift 2
      ;;
    --source-ref-kind)
      source_ref_kind="$2"
      shift 2
      ;;
    --no-fetch)
      fetch=0
      shift
      ;;
    --compare-only)
      compare_only=1
      shift
      ;;
    --json)
      json_output=1
      shift
      ;;
    --verbose)
      verbose=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${commit}" ]]; then
  echo "--commit is required" >&2
  usage >&2
  exit 2
fi
if [[ ! -d "${baseline}" ]]; then
  echo "baseline directory does not exist: ${baseline}" >&2
  exit 1
fi
if [[ "${compare_only}" -eq 1 ]]; then
  if [[ -z "${candidate}" ]]; then
    echo "--candidate is required with --compare-only" >&2
    usage >&2
    exit 2
  fi
  if [[ ! -d "${candidate}" ]]; then
    echo "candidate directory does not exist: ${candidate}" >&2
    exit 1
  fi
elif [[ -z "${codex_repo}" ]]; then
  echo "--codex-repo is required unless CODEXSDK_CODEX_REPO is set" >&2
  usage >&2
  exit 2
elif [[ ! -d "${codex_repo}/.git" ]]; then
  echo "codex repo is not a git repository: ${codex_repo}" >&2
  exit 1
fi
if [[ "${compare_only}" -eq 0 && "${generator}" != "binary" && "${generator}" != "cargo" ]]; then
  echo "--generator must be binary or cargo" >&2
  exit 2
fi
if [[ -z "${out}" ]]; then
  out="$(mktemp -d "${TMPDIR:-/tmp}/codexsdk-upstream.XXXXXX")"
fi

resolve_target_ref() {
  local ref="$1"
  local candidate=""
  local candidates=("${ref}")

  case "${ref}" in
    refs/tags/*)
      candidates+=("${ref}")
      ;;
    refs/heads/*)
      candidates+=("refs/remotes/origin/${ref#refs/heads/}")
      ;;
    *)
      candidates+=("refs/tags/${ref}" "refs/remotes/origin/${ref}")
      ;;
  esac

  for candidate in "${candidates[@]}"; do
    if git -C "${codex_repo}" rev-parse --verify -q "${candidate}^{commit}"; then
      return 0
    fi
  done
  return 1
}

fetch_target_ref() {
  local ref="$1"

  if [[ "${ref}" =~ ^[0-9a-f]{40}$ ]]; then
    git -C "${codex_repo}" fetch origin "${ref}"
    return
  fi

  case "${ref}" in
    refs/tags/*)
      git -C "${codex_repo}" fetch origin "${ref}:${ref}"
      ;;
    refs/heads/*)
      git -C "${codex_repo}" fetch origin "${ref}:refs/remotes/origin/${ref#refs/heads/}"
      ;;
    *)
      git -C "${codex_repo}" fetch origin "refs/tags/${ref}:refs/tags/${ref}" 2>/dev/null \
        || git -C "${codex_repo}" fetch origin "refs/heads/${ref}:refs/remotes/origin/${ref}" 2>/dev/null \
        || git -C "${codex_repo}" fetch origin "${ref}"
      ;;
  esac
}

if [[ "${compare_only}" -eq 1 ]]; then
  resolved_commit="${commit}"
else
  if [[ "${fetch}" -eq 1 ]] && ! resolve_target_ref "${commit}" >/dev/null; then
    fetch_target_ref "${commit}"
  fi
  if ! resolved_commit="$(resolve_target_ref "${commit}")"; then
    echo "unable to resolve Codex target as a commit: ${commit}" >&2
    exit 1
  fi
fi
if [[ -z "${source_ref}" ]]; then
  source_ref="${commit}"
fi
if [[ -z "${source_ref_kind}" ]]; then
  if [[ "${source_ref}" =~ ^rust-v[0-9]+[.][0-9]+[.][0-9]+$ ]]; then
    source_ref_kind="stable_rust_tag"
  elif [[ "${source_ref}" =~ ^[0-9a-f]{40}$ ]]; then
    source_ref_kind="manual_commit"
  else
    source_ref_kind="manual_ref"
  fi
fi
reports="${out}/reports"
if [[ "${compare_only}" -eq 1 ]]; then
  generated="${candidate}"
else
  generated="${out}/schema"
  stable_generated="${out}/stable-schema"
fi
if [[ "${generated}" == "/" || "${reports}" == "/" ]]; then
  echo "refusing to use unsafe output paths under --out=${out}" >&2
  exit 1
fi
if [[ "${compare_only}" -eq 1 ]]; then
  rm -rf "${reports}"
  mkdir -p "${reports}"
  codex_version="compare-only"
  generator="compare-only"
  generator_detail="candidate schema: ${generated}"
else
  rm -rf "${generated}" "${stable_generated}" "${reports}"
  mkdir -p "${generated}" "${stable_generated}" "${reports}"

  case "${generator}" in
    binary)
      if [[ "${verbose}" -eq 1 ]]; then
        echo "generating schema with binary: ${codex_bin}" >&2
      fi
      "${codex_bin}" app-server generate-json-schema --experimental --out "${generated}" 1>&2
      "${codex_bin}" app-server generate-json-schema --out "${stable_generated}" 1>&2
      codex_version="$("${codex_bin}" --version 2>/dev/null || true)"
      generator_detail="${codex_bin}"
      ;;
    cargo)
      worktree="${out}/codex-worktree"
      if [[ "${verbose}" -eq 1 ]]; then
        echo "creating Codex worktree: ${worktree}" >&2
      fi
      git -C "${codex_repo}" worktree add --detach "${worktree}" "${resolved_commit}" >/dev/null
      (
        cd "${worktree}/codex-rs"
        cargo run -p codex-cli -- app-server generate-json-schema --experimental --out "${generated}" 1>&2
        cargo run -p codex-cli -- app-server generate-json-schema --out "${stable_generated}" 1>&2
      )
      codex_version="$(
        cd "${worktree}/codex-rs"
        cargo run -p codex-cli -- --version 2>/dev/null || true
      )"
      generator_detail="${worktree}/codex-rs cargo run -p codex-cli"
      ;;
  esac
fi

schema_diff_args=(
  --baseline "${baseline}" \
  --candidate "${generated}" \
  --reports "${reports}" \
  --source-commit "${resolved_commit}" \
  --source-ref "${source_ref}" \
  --source-ref-kind "${source_ref_kind}" \
  --codex-version "${codex_version}" \
  --generator "${generator}" \
  --generator-detail "${generator_detail}"
)
if [[ "${verbose}" -eq 1 ]]; then
  schema_diff_args+=(--verbose)
fi

python3 "${script_dir}/codexsdk_schema_diff.py" "${schema_diff_args[@]}"

if [[ "${verbose}" -eq 1 ]]; then
  echo "generated schema: ${generated}" >&2
  echo "reports: ${reports}" >&2
fi

if [[ "${json_output}" -eq 1 ]]; then
  python3 - "${generated}" "${reports}" "${resolved_commit}" <<'PY'
import json
import pathlib
import sys

schema = sys.argv[1]
reports = pathlib.Path(sys.argv[2])
source_commit = sys.argv[3]
drift = json.loads((reports / "drift_summary.json").read_text(encoding="utf-8"))
print(json.dumps({
    "reports": str(reports),
    "schema": schema,
    "source_commit": source_commit,
    "status": drift["status"],
}, sort_keys=True))
PY
fi
