#!/usr/bin/env bash
set -euo pipefail

project_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-rpc-install-test.XXXXXX")"
module_cache="$(go env GOMODCACHE)"
cleanup() {
    rm -rf -- "${test_root}"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "${test_root}/fake-bin"
# The generated script, rather than this test process, expands these variables.
# shellcheck disable=SC2016
printf '%s\n' \
    '#!/usr/bin/env bash' \
    'printf "%s\n" "$*" >> "$SYSTEMCTL_LOG"' \
    'case "$*" in' \
    '  "--user daemon-reload"|"--user disable --now codex-discord-rpc.service") exit 0 ;;' \
    '  *) echo "unexpected systemctl call: $*" >&2; exit 99 ;;' \
    'esac' > "${test_root}/fake-bin/systemctl"
chmod 0755 "${test_root}/fake-bin/systemctl"

environment=(
    "HOME=${test_root}/home"
    "PATH=${test_root}/fake-bin:${PATH}"
    "CODEX_RPC_INSTALL_ROOT=${test_root}/share"
    "CODEX_RPC_BIN_DIR=${test_root}/bin"
    "CODEX_RPC_SYSTEMD_DIR=${test_root}/systemd"
    "GOMODCACHE=${module_cache}"
    "SYSTEMCTL_LOG=${test_root}/systemctl.log"
)
mkdir -p "${test_root}/unsafe-home"
if env "HOME=${test_root}/unsafe-home" \
    "CODEX_RPC_INSTALL_ROOT=${test_root}/unsafe-home" \
    bash "${project_dir}/scripts/install.sh" >/dev/null 2>&1; then
    echo 'installer accepted HOME as its install root' >&2
    exit 1
fi
mkdir -p "${test_root}/unmanaged"
printf 'keep\n' > "${test_root}/unmanaged/sentinel"
if env "${environment[@]}" "CODEX_RPC_INSTALL_ROOT=${test_root}/unmanaged" \
    bash "${project_dir}/scripts/install.sh" >/dev/null 2>&1; then
    echo 'installer accepted an unmarked install root' >&2
    exit 1
fi
test "$(cat "${test_root}/unmanaged/sentinel")" = keep

mkdir -p "${test_root}/unsafe-managed/bin/codex-rpc"
touch "${test_root}/unsafe-managed/.installed-by-codex-discord-rpc"
if env "${environment[@]}" "CODEX_RPC_INSTALL_ROOT=${test_root}/unsafe-managed" \
    bash "${project_dir}/scripts/install.sh" >/dev/null 2>&1; then
    echo 'installer accepted a directory in place of its managed binary' >&2
    exit 1
fi
test -d "${test_root}/unsafe-managed/bin/codex-rpc"

env "${environment[@]}" bash "${project_dir}/scripts/install.sh"
test ! -e "${test_root}/systemctl.log"
test "$(env "${environment[@]}" "${test_root}/bin/codex-rpc" --rpc-version)" = '0.2.0'
test -x "${test_root}/share/bin/codex-rpc"
test -L "${test_root}/bin/codex-rpc"
cmp "${project_dir}/systemd/codex-discord-rpc.service" \
    "${test_root}/share/systemd/codex-discord-rpc.service"
test ! -e "${test_root}/systemd/codex-discord-rpc.service"

env "${environment[@]}" "${test_root}/bin/codex-rpc" service install
test "$(sed -n '1p' "${test_root}/systemctl.log")" = '--user daemon-reload'
cmp "${project_dir}/systemd/codex-discord-rpc.service" \
    "${test_root}/systemd/codex-discord-rpc.service"
mkdir -p "${test_root}/systemd/graphical-session.target.wants"
ln -s ../codex-discord-rpc.service \
    "${test_root}/systemd/graphical-session.target.wants/codex-discord-rpc.service"
env "${environment[@]}" "${test_root}/bin/codex-rpc" service uninstall
test "$(sed -n '2p' "${test_root}/systemctl.log")" = \
    '--user disable --now codex-discord-rpc.service'
test "$(sed -n '3p' "${test_root}/systemctl.log")" = '--user daemon-reload'
test "$(wc -l < "${test_root}/systemctl.log")" -eq 3
test ! -e "${test_root}/systemd/graphical-session.target.wants/codex-discord-rpc.service"

# Repeating uninstall is a no-op and must not invoke systemctl again.
env "${environment[@]}" "${test_root}/bin/codex-rpc" service uninstall
test "$(wc -l < "${test_root}/systemctl.log")" -eq 3

env "${environment[@]}" bash "${project_dir}/scripts/uninstall.sh"
test ! -e "${test_root}/share"
test ! -e "${test_root}/bin/codex-rpc"
test ! -e "${test_root}/systemd/codex-discord-rpc.service"
