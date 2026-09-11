#!/usr/bin/env bash
set -euo pipefail

: "${HOME:?HOME must be set}"

project_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
home_path="$(realpath -m -- "${HOME}")"
install_root="$(realpath -m -- "${CODEX_RPC_INSTALL_ROOT:-${HOME}/.local/share/codex-discord-rpc}")"
bin_dir="$(realpath -m -- "${CODEX_RPC_BIN_DIR:-${HOME}/.local/bin}")"
launcher="${install_root}/bin/codex-rpc"
command_path="${bin_dir}/codex-rpc"
marker="${install_root}/.installed-by-codex-discord-rpc"
go_command="${GO:-go}"

validate_directory() {
    local label="$1"
    local path="$2"
    if [[ -z "${path}" || "${path}" == / ]]; then
        printf 'Refusing unsafe %s: %s\n' "${label}" "${path}" >&2
        exit 2
    fi
}

validate_directory 'install directory' "${install_root}"
validate_directory 'binary directory' "${bin_dir}"
if [[ "${home_path}" == "${install_root}" || "${home_path}" == "${install_root}/"* ]]; then
    printf 'Refusing install directory that contains HOME: %s\n' "${install_root}" >&2
    exit 2
fi
if [[ -e "${install_root}" || -L "${install_root}" ]]; then
    if [[ ! -f "${marker}" || -L "${marker}" ]]; then
        printf 'Refusing to modify unmarked install directory: %s\n' "${install_root}" >&2
        exit 2
    fi
fi
for managed_directory in "${install_root}/bin" "${install_root}/systemd"; do
    if [[ -e "${managed_directory}" || -L "${managed_directory}" ]]; then
        if [[ ! -d "${managed_directory}" || -L "${managed_directory}" ]]; then
            printf 'Refusing unsafe managed directory: %s\n' "${managed_directory}" >&2
            exit 2
        fi
    fi
done
for managed_file in "${launcher}" "${install_root}/systemd/codex-discord-rpc.service"; do
    if [[ -e "${managed_file}" || -L "${managed_file}" ]]; then
        if [[ ! -f "${managed_file}" || -L "${managed_file}" ]]; then
            printf 'Refusing unsafe managed file: %s\n' "${managed_file}" >&2
            exit 2
        fi
    fi
done
if [[ -e "${command_path}" || -L "${command_path}" ]]; then
    if [[ ! -L "${command_path}" || "$(readlink -- "${command_path}")" != "${launcher}" ]]; then
        printf 'Refusing to replace unmanaged command: %s\n' "${command_path}" >&2
        exit 2
    fi
fi
if ! command -v "${go_command}" >/dev/null 2>&1; then
    printf 'Go compiler not found: %s\n' "${go_command}" >&2
    exit 127
fi

build_dir="$(mktemp -d "${TMPDIR:-/tmp}/codex-rpc-build.XXXXXX")"
launcher_temporary=''
unit_temporary=''
cleanup() {
    if [[ -n "${launcher_temporary}" ]]; then
        rm -f -- "${launcher_temporary}"
    fi
    if [[ -n "${unit_temporary}" ]]; then
        rm -f -- "${unit_temporary}"
    fi
    rm -rf -- "${build_dir}"
}
trap cleanup EXIT HUP INT TERM

(
    cd "${project_dir}"
    CGO_ENABLED=0 "${go_command}" build -trimpath -ldflags='-s -w' \
        -o "${build_dir}/codex-rpc" ./cmd/codex-rpc
)

mkdir -p "${install_root}/bin" "${install_root}/systemd" "${bin_dir}"
launcher_temporary="$(mktemp "${install_root}/bin/.codex-rpc.XXXXXX")"
install -m 0755 "${build_dir}/codex-rpc" "${launcher_temporary}"
mv -f -- "${launcher_temporary}" "${launcher}"
launcher_temporary=''
unit_temporary="$(mktemp "${install_root}/systemd/.codex-discord-rpc.service.XXXXXX")"
install -m 0644 "${project_dir}/systemd/codex-discord-rpc.service" \
    "${unit_temporary}"
mv -f -- "${unit_temporary}" "${install_root}/systemd/codex-discord-rpc.service"
unit_temporary=''
touch "${marker}"
if [[ ! -L "${command_path}" ]]; then
    ln -s "${launcher}" "${command_path}"
fi
printf 'Installed Go binary: %s -> %s\n' "${command_path}" "${launcher}"
printf 'Packaged user unit: %s\n' "${install_root}/systemd/codex-discord-rpc.service"
printf 'No service was enabled, started, stopped, or restarted.\n'
printf 'After reviewing the unit, run these explicit commands if desired:\n'
printf '  codex-rpc service install\n'
printf '  codex-rpc service enable\n'
printf '  codex-rpc service start     # fresh install\n'
printf '  codex-rpc service restart   # explicit upgrade of an active service\n'
