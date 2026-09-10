#!/usr/bin/env bash
set -euo pipefail

project_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
install_root="${CODEX_RPC_INSTALL_ROOT:-${HOME}/.local/share/codex-discord-rpc}"
bin_dir="${CODEX_RPC_BIN_DIR:-${HOME}/.local/bin}"
launcher="${install_root}/bin/codex-rpc"
command_path="${bin_dir}/codex-rpc"
systemd_dir="${CODEX_RPC_SYSTEMD_DIR:-${HOME}/.config/systemd/user}"
service_path="${systemd_dir}/codex-discord-rpc.service"
enable_service="${CODEX_RPC_ENABLE_SERVICE:-auto}"

mkdir -p "${install_root}/src" "${install_root}/bin" "${install_root}/systemd" "${bin_dir}"
cp -a "${project_dir}/src/." "${install_root}/src/"
cp "${project_dir}/bin/codex-rpc" "${launcher}"
cp "${project_dir}/systemd/codex-discord-rpc.service" "${install_root}/systemd/codex-discord-rpc.service"
chmod 755 "${launcher}"
touch "${install_root}/.installed-by-codex-discord-rpc"

if [[ -e "${command_path}" && ! -L "${command_path}" ]]; then
    printf 'Ошибка: %s уже существует и не является символьной ссылкой.\n' "${command_path}" >&2
    printf 'Переместите файл вручную и повторите установку.\n' >&2
    exit 1
fi
ln -sfn "${launcher}" "${command_path}"

printf 'Установлено: %s -> %s\n' "${command_path}" "${launcher}"
if [[ ":${PATH}:" != *":${bin_dir}:"* ]]; then
    printf 'Добавьте %s в PATH, если команда codex-rpc не находится.\n' "${bin_dir}"
fi

mkdir -p "${systemd_dir}"
cp "${project_dir}/systemd/codex-discord-rpc.service" "${service_path}"
chmod 644 "${service_path}"
printf 'Установлен user service: %s\n' "${service_path}"

if [[ "${enable_service}" != "0" ]]; then
    if command -v systemctl >/dev/null 2>&1 \
        && systemctl --user daemon-reload >/dev/null 2>&1 \
        && systemctl --user enable codex-discord-rpc.service >/dev/null 2>&1 \
        && systemctl --user restart codex-discord-rpc.service >/dev/null 2>&1; then
        printf 'Сервис codex-discord-rpc включён и запущен.\n'
    else
        printf 'Не удалось автоматически запустить user service. Выполните:\n' >&2
        printf '  systemctl --user daemon-reload\n' >&2
        printf '  systemctl --user enable --now codex-discord-rpc.service\n' >&2
    fi
else
    printf 'Автозапуск user service отключён (CODEX_RPC_ENABLE_SERVICE=0).\n'
fi
