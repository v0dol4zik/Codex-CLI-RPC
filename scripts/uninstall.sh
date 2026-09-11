#!/usr/bin/env bash
set -euo pipefail

: "${HOME:?HOME must be set}"

home_path="$(realpath -m -- "${HOME}")"
install_root="$(realpath -m -- "${CODEX_RPC_INSTALL_ROOT:-${HOME}/.local/share/codex-discord-rpc}")"
bin_dir="$(realpath -m -- "${CODEX_RPC_BIN_DIR:-${HOME}/.local/bin}")"
systemd_dir="$(realpath -m -- "${CODEX_RPC_SYSTEMD_DIR:-${HOME}/.config/systemd/user}")"
launcher="${install_root}/bin/codex-rpc"
command_path="${bin_dir}/codex-rpc"
service_path="${systemd_dir}/codex-discord-rpc.service"
marker="${install_root}/.installed-by-codex-discord-rpc"
graphical_link="${systemd_dir}/graphical-session.target.wants/codex-discord-rpc.service"
legacy_link="${systemd_dir}/default.target.wants/codex-discord-rpc.service"

if [[ -z "${install_root}" || "${install_root}" == / \
    || "${home_path}" == "${install_root}" || "${home_path}" == "${install_root}/"* ]]; then
    printf 'Refusing unsafe install directory: %s\n' "${install_root}" >&2
    exit 2
fi
if [[ -z "${bin_dir}" || "${bin_dir}" == / \
    || -z "${systemd_dir}" || "${systemd_dir}" == / ]]; then
    printf 'Refusing unsafe binary or systemd directory.\n' >&2
    exit 2
fi
if [[ -e "${graphical_link}" || -L "${graphical_link}" \
    || -e "${legacy_link}" || -L "${legacy_link}" \
    || -e "${service_path}" || -L "${service_path}" ]]; then
    printf 'User service is still installed or enabled. Run codex-rpc service uninstall first.\n' >&2
    exit 2
fi
if [[ ! -f "${marker}" || -L "${marker}" ]]; then
    printf 'Refusing to remove unmarked install directory: %s\n' "${install_root}" >&2
    exit 2
fi
if [[ -L "${command_path}" && "$(readlink -- "${command_path}")" == "${launcher}" ]]; then
    rm -f -- "${command_path}"
fi
rm -rf -- "${install_root}"

printf 'Codex Discord RPC files removed. No systemctl command was run.\n'
printf 'User configuration was preserved.\n'
