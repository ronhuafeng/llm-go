#!/usr/bin/env python3
"""Plan the deterministic execution mode for the protocol sync workflow."""

from __future__ import annotations

import argparse
import json
import os


def parse_bool(value: str) -> bool:
    if value.lower() in {"1", "true", "yes", "on"}:
        return True
    if value.lower() in {"0", "false", "no", "off"}:
        return False
    raise argparse.ArgumentTypeError(f"invalid boolean value: {value!r}")


def result(
    mode: str,
    *,
    should_detect: bool = False,
    verification_failed: bool = False,
    needs_apply: bool = False,
    needs_agent: bool = False,
    needs_publish: bool = False,
) -> dict[str, object]:
    return {
        "mode": mode,
        "needs_agent": needs_agent,
        "needs_apply": needs_apply,
        "needs_publish": needs_publish,
        "should_detect": should_detect,
        "verification_failed": verification_failed,
    }


def plan_workflow(
    *,
    policy_decision: str,
    force_compare: bool,
    drift_status: str = "",
) -> dict[str, object]:
    if force_compare and policy_decision == "block":
        return result("verification-fail", verification_failed=True)
    if force_compare and policy_decision in {"allow", "skip"}:
        if drift_status == "review-required":
            return result(
                "verification-fail",
                should_detect=True,
                verification_failed=True,
            )
        return result("verify", should_detect=True)
    if not drift_status and policy_decision == "skip":
        return result("noop")
    if not drift_status and policy_decision == "block":
        return result("blocked")
    if not drift_status and policy_decision == "allow":
        return result("detect", should_detect=True)
    if policy_decision == "allow" and drift_status == "clean":
        return result(
            "metadata-sync",
            should_detect=True,
            needs_apply=True,
            needs_publish=True,
        )
    if policy_decision == "allow" and drift_status == "review-required":
        return result(
            "repair-sync",
            should_detect=True,
            needs_apply=True,
            needs_agent=True,
            needs_publish=True,
        )
    raise ValueError("unsupported workflow planning state")


def write_github_output(payload: dict[str, object]) -> None:
    output_path = os.environ.get("GITHUB_OUTPUT")
    if not output_path:
        return
    with open(output_path, "a", encoding="utf-8") as output:
        for key, value in payload.items():
            rendered = str(value).lower() if isinstance(value, bool) else str(value)
            output.write(f"{key}={rendered}\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--policy-decision", choices=("allow", "skip", "block"), required=True)
    parser.add_argument("--force-compare", type=parse_bool, required=True)
    parser.add_argument("--drift-status", choices=("clean", "review-required"), default="")
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    payload = plan_workflow(
        policy_decision=args.policy_decision,
        force_compare=args.force_compare,
        drift_status=args.drift_status,
    )
    write_github_output(payload)
    if args.json:
        print(json.dumps(payload, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
