#!/usr/bin/env bash
# CI-only compatibility for Ubuntu 26.04's distro Bubblewrap AppArmor policy.
# This is restricted to disposable GitHub-hosted VMs, before provider or
# repository-write credentials are supplied to a native goal. Never run on a
# persistent host. Fail closed on unexpected hosts or probe failures.
set -euo pipefail

if [[ "${GITHUB_ACTIONS:-}" != "true" || "${HOSTED_ENVIRONMENT:-}" != "github-hosted" ]]; then
  echo "::error::native sandbox setup requires a GitHub-hosted ephemeral runner"
  exit 1
fi
# shellcheck disable=SC1091
source /etc/os-release
if [[ "${ID:-}" != "ubuntu" || "${VERSION_ID:-}" != "26.04" ]]; then
  echo "::error::native sandbox setup requires the verified Ubuntu 26.04 image"
  exit 1
fi
if [[ "$(readlink -f "$(command -v bwrap)")" != "/usr/bin/bwrap" ]]; then
  echo "::error::native sandbox setup requires distro /usr/bin/bwrap"
  exit 1
fi
if ! command -v apparmor_parser >/dev/null; then
  echo "::error::native sandbox setup requires AppArmor tooling"
  exit 1
fi
userns_restrict="$(sysctl -n kernel.apparmor_restrict_unprivileged_userns)"
if [[ "$userns_restrict" != 0 ]]; then
  echo "::error::native sandbox setup requires the existing userns sysctl prerequisite"
  exit 1
fi
# Ubuntu 26.04 also restricts unprivileged unconfined descendants, including
# nested user namespaces after the distro bwrap profile is unloaded. On this
# disposable CI runner only, enable that path before verifying BOTH boundaries.
if [[ -r /proc/sys/kernel/apparmor_restrict_unprivileged_unconfined ]]; then
  sudo sysctl -w kernel.apparmor_restrict_unprivileged_unconfined=0
fi

echo "::group::Native sandbox host diagnostics (public fixed facts)"
echo "runner_os=ubuntu-26.04"
echo "kernel_release=$(uname -r)"
echo "bubblewrap_version=$(bwrap --version)"
echo "apparmor_userns_restrict=$userns_restrict"
if [[ -r /proc/sys/kernel/apparmor_restrict_unprivileged_unconfined ]]; then
  echo "apparmor_unconfined_restrict=$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_unconfined)"
fi
echo "::endgroup::"

error_file="$(mktemp)"
trap 'rm -f "$error_file"' EXIT
classify_error() {
  # Never publish raw sandbox stderr: it may contain private paths or prompts.
  if grep -Fq 'No permissions to create' "$error_file"; then echo user_namespace_denied
  elif grep -Fq 'setting up uid map' "$error_file"; then echo uid_map_denied
  elif grep -Fq 'Failed RTM_NEWADDR' "$error_file"; then echo network_capability_denied
  elif grep -Fq 'Operation not permitted' "$error_file"; then echo operation_not_permitted
  elif grep -Fq 'Permission denied' "$error_file"; then echo permission_denied
  elif grep -Fq 'bwrap:' "$error_file"; then echo bubblewrap_other
  elif [[ -s "$error_file" ]]; then echo other_stderr
  else echo no_stderr
  fi
}
probe() {
  local name="$1"
  shift
  : > "$error_file"
  if timeout 10s "$@" >/dev/null 2>"$error_file"; then
    echo "native_probe=$name status=pass"
  else
    local code=$?
    echo "::error::native_probe=$name status=fail exit_code=$code reason=$(classify_error)"
    return 1
  fi
}

# This initial probe is observational: failure under the loaded distro profile
# is permitted here, but never treated as a successful final sandbox proof.
: > "$error_file"
if timeout 10s /usr/bin/bwrap --unshare-user --ro-bind / / -- /usr/bin/true >/dev/null 2>"$error_file"; then
  echo "native_probe=before_profile_change status=pass"
else
  echo "native_probe=before_profile_change status=blocked reason=$(classify_error)"
fi

# Ubuntu 26.04's loaded bwrap-userns-restrict transitions descendants into
# unpriv_bwrap, which denies nested capabilities. The sysctl alone does not
# clear the loaded profile. Only on the disposable GitHub-hosted VM, unload it
# in memory (never alter profile files). This relaxes the CI host's AppArmor
# restriction; downstream native tests MUST still prove workspace, credential
# and network isolation. Never use this workaround on persistent hosts.
profile=/etc/apparmor.d/bwrap-userns-restrict
if [[ -f "$profile" ]]; then
  sudo apparmor_parser -R "$profile"
  echo "native_apparmor_bwrap_profile=unloaded_for_ephemeral_ci"
else
  echo "native_apparmor_bwrap_profile=not_installed"
fi

probe outer /usr/bin/bwrap --unshare-user --ro-bind / / -- /usr/bin/true
probe nested /usr/bin/bwrap --unshare-user --ro-bind / / -- \
  /usr/bin/bwrap --unshare-user --ro-bind / / -- /usr/bin/true
