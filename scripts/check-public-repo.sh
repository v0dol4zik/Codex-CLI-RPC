#!/usr/bin/env bash
set -euo pipefail

project_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
scan_args=(
    --hidden
    --glob '!.git/**'
    --glob '!**/__pycache__/**'
    --glob '!**/check-public-repo.sh'
)
failed=0

check_pattern() {
    local description="$1"
    local pattern="$2"
    if rg -n "${scan_args[@]}" --pcre2 -- "${pattern}" "${project_dir}"; then
        printf 'Обнаружено потенциально приватное значение: %s\n' "${description}" >&2
        failed=1
    fi
}

check_pattern 'числовой Discord Application ID' '(?<![0-9])[0-9]{17,20}(?![0-9])'
check_pattern '64-символьный hex secret' '(?<![A-Fa-f0-9])[A-Fa-f0-9]{64}(?![A-Fa-f0-9])'
check_pattern 'заполненный token/secret' '(?i)(bot_token|client_secret|discord_token)[[:space:]]*=[[:space:]]*["'"'][^"'"']+["'"']'
check_pattern 'персональный абсолютный home path' '/home/(?!user(?:/|$))[^/[:space:]]+/'

exit "${failed}"
