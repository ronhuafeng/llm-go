#!/usr/bin/env python3
"""Write the bounded Codex repair prompt used by the upstream protocol sync workflow."""

from __future__ import annotations

import argparse
from pathlib import Path
from string import Template


REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_TEMPLATE = REPO_ROOT / ".github/prompts/codexsdk-upstream-sync-repair.md"


def build_prompt(
    *,
    auto_sync_dir: str,
    candidate_dir: str,
    land_ref: str,
    phase: str = "fix",
    template: Path = DEFAULT_TEMPLATE,
    upstream_ref: str,
    upstream_ref_kind: str,
    upstream_sha: str,
) -> str:
    return render_prompt(
        template.read_text(encoding="utf-8"),
        auto_sync_dir=auto_sync_dir,
        candidate_dir=candidate_dir,
        land_ref=land_ref,
        phase=phase,
        upstream_ref=upstream_ref,
        upstream_ref_kind=upstream_ref_kind,
        upstream_sha=upstream_sha,
    )


def render_prompt(
    template_text: str,
    *,
    auto_sync_dir: str,
    candidate_dir: str,
    land_ref: str,
    phase: str = "fix",
    upstream_ref: str,
    upstream_ref_kind: str,
    upstream_sha: str,
) -> str:
    prompt = Template(template_text).substitute(
        AUTO_SYNC_DIR=auto_sync_dir,
        CANDIDATE_DIR=candidate_dir,
        LAND_REF=land_ref,
        PHASE=phase,
        UPSTREAM_REF=upstream_ref,
        UPSTREAM_REF_KIND=upstream_ref_kind,
        UPSTREAM_SHA=upstream_sha,
    )
    if not prompt.endswith("\n"):
        prompt += "\n"
    return prompt


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, type=Path, help="prompt file to write")
    parser.add_argument("--auto-sync-dir", required=True, help="directory containing mechanical apply summaries")
    parser.add_argument("--candidate-dir", required=True, help="downloaded upstream sync candidate directory")
    parser.add_argument("--land-ref", required=True, help="landing branch/ref")
    parser.add_argument("--phase", default="fix", help="current workflow phase included in the prompt")
    parser.add_argument("--template", type=Path, default=DEFAULT_TEMPLATE, help="Markdown prompt template")
    parser.add_argument("--upstream-ref", required=True, help="selected upstream ref")
    parser.add_argument("--upstream-ref-kind", required=True, help="selected upstream ref kind")
    parser.add_argument("--upstream-sha", required=True, help="selected upstream commit SHA")
    args = parser.parse_args()

    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(
        build_prompt(
            auto_sync_dir=args.auto_sync_dir,
            candidate_dir=args.candidate_dir,
            land_ref=args.land_ref,
            phase=args.phase,
            template=args.template,
            upstream_ref=args.upstream_ref,
            upstream_ref_kind=args.upstream_ref_kind,
            upstream_sha=args.upstream_sha,
        ),
        encoding="utf-8",
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
