#!/usr/bin/env bash
set -euo pipefail

install_root="${CODEX_RPC_INSTALL_ROOT:-${HOME}/.local/share/codex-discord-rpc}"
bin_dir="${CODEX_RPC_BIN_DIR:-${HOME}/.local/bin}"
systemd_dir="${CODEX_RPC_SYSTEMD_DIR:-${HOME}/.config/systemd/user}"
launcher="${install_root}/bin/codex-rpc"
command_path="${bin_dir}/codex-rpc"
service_path="${systemd_dir}/codex-discord-rpc.service"
marker="${install_root}/.installed-by-codex-discord-rpc"
manage_service="${CODEX_RPC_MANAGE_SERVICE:-1}"

if [[ -z "${install_root}" || "${install_root}" == "/" || "${install_root}" == "${HOME}" ]]; then
    printf 'Отказ: небезопасный каталог установки: %s\n' "${install_root}" >&2
    exit 2
fi

if [[ "${manage_service}" != "0" ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl --user disable --now codex-discord-rpc.service >/dev/null 2>&1 || true
fi

if [[ -f "${service_path}" ]]; then
    rm -f -- "${service_path}"
fi
if [[ "${manage_service}" != "0" ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl --user daemon-reload >/dev/null 2>&1 || true
fi

if [[ -L "${command_path}" && "$(readlink -f -- "${command_path}")" == "$(readlink -f -- "${launcher}")" ]]; then
    rm -f -- "${command_path}"
fi

if [[ -f "${marker}" ]]; then
    rm -rf -- "${install_root}"
else
    printf 'Каталог %s не удалён: отсутствует маркер установщика.\n' "${install_root}" >&2
fi

printf 'Codex Discord RPC удалён. Пользовательский config сохранён.\n'
