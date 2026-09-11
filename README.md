# Codex CLI Discord RPC

```ascii
_________     _________                _________________________
__  ____/___________  /________  __    ___  __ \__  __ \_  ____/
_  /    _  __ \  __  /_  _ \_  |/_/    __  /_/ /_  /_/ /  /
/ /___  / /_/ / /_/ / /  __/_>  <      _  _, _/_  ____// /___
\____/  \____/\__,_/  \___//_/|_|      /_/ |_| /_/     \____/
```

[![AI Slop Inside](https://sladge.net/badge.svg)](https://sladge.net)
[![Platform: Linux](https://img.shields.io/badge/platform-Linux-FCC624?logo=linux&logoColor=black)](#requirements)
[![CI](https://github.com/v0dol4zik/Codex-CLI-RPC/actions/workflows/ci.yml/badge.svg)](https://github.com/v0dol4zik/Codex-CLI-RPC/actions/workflows/ci.yml)
[![Go 1.23+](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Runtime dependencies: none](https://img.shields.io/badge/runtime_dependencies-none-2ea44f)](go.mod)
[![Service: systemd](https://img.shields.io/badge/service-systemd-0086CC?logo=systemd&logoColor=white)](systemd/codex-discord-rpc.service)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**A small Go program for Linux that shows how long Codex CLI has been running in Discord Rich Presence.**

After installation, a user-level systemd service monitors `codex` processes
and displays an Activity in Discord.

## Features

- the timer starts when `codex` is actually launched and resets when it closes;
- a Codex process already running when the monitor starts is detected;
- closing the Discord client does not reset the timer;
- a lock file prevents a second RPC client from starting;
- if several `codex` processes are running, the newest one is selected;
- Codex continues working when Discord is unavailable;
- one statically linked Go binary keeps runtime memory and startup overhead low;
- idle process scans back off to five seconds to reduce wakeups when Codex is not running;
- no Bot Token, external server, or OpenAI API key is used.

## Requirements

- a Linux distribution that supports the Discord desktop client;
- Go 1.23 or newer to build from source;
- Codex CLI, with the `codex` command available in `PATH`;
- a Discord Application ID from the Developer Portal.

## Installation

```bash
git clone https://github.com/v0dol4zik/Codex-CLI-RPC.git
cd Codex-CLI-RPC

mkdir -p ~/.config/codex-discord-rpc
cp config.example.toml ~/.config/codex-discord-rpc/config.toml
$EDITOR ~/.config/codex-discord-rpc/config.toml

./scripts/install.sh
```

Get a Discord Application ID and replace the value in `config.toml` with your
own. For the background systemd service, the ID must be stored in the config
file, not only in a variable in your shell environment.

The installer builds a static Go binary, places it in
`~/.local/share/codex-discord-rpc`, creates `~/.local/bin/codex-rpc`, and
packages the unit at:

```text
~/.local/share/codex-discord-rpc/systemd/codex-discord-rpc.service
```

It deliberately does not call `systemctl` or change the running service.
Review [the unit](systemd/codex-discord-rpc.service), then explicitly install,
enable, and start it:

```bash
codex-rpc service install
codex-rpc service enable
codex-rpc service start     # fresh install
# or, when explicitly upgrading an active older service:
codex-rpc service restart
```

These commands are separate by design. `service install` only atomically
writes the unit, removes the obsolete v0.1.0 `default.target` symlink when it
is actually a symlink, and runs `daemon-reload`. It never enables, starts,
stops, or restarts the service.

## Running and managing

After installation, you can start Codex and check the RPC:

```bash
codex
```

Check the RPC and systemd service:

```bash
codex-rpc --check
codex-rpc --rpc-version
codex-rpc service status
```

Manage the user service:

```bash
codex-rpc service install
codex-rpc service enable
codex-rpc service disable
codex-rpc service start
codex-rpc service restart
codex-rpc service stop
codex-rpc service uninstall
```

The unit is bound to `graphical-session.target`, so it starts and stops with
the desktop session without pulling that target into a TTY login. It uses
`Restart=on-failure`, restart-rate limiting, `MemoryMax=64M`, `TasksMax=64`,
`NoNewPrivileges=yes`, and Unix-socket-only networking.

If the systemd service is unavailable, you can start the monitor manually:

```bash
codex-rpc --monitor
```

No shell alias is required: the background monitor detects the ordinary
`codex` command, and the lock prevents duplicate RPC clients. Wrapper mode is
kept for manual launches and compatibility:

```bash
codex-rpc exec "check the tests"
```

## Configuration

The program's main settings are stored in
`~/.config/codex-discord-rpc/config.toml`. Environment variables take
precedence over the TOML file.

| Parameter                                                 | Purpose                            | Default                |
| --------------------------------------------------------- | ---------------------------------- | ---------------------- |
| `client_id` / `CODEX_DISCORD_CLIENT_ID`                   | Discord Application ID             | Presence disabled      |
| `codex_binary` / `CODEX_BINARY`                           | Path to Codex CLI                  | find `codex` in `PATH` |
| `details` / `CODEX_RPC_DETAILS`                           | Main Activity line                 | Russian text meaning `Working in Codex` |
| `state` / `CODEX_RPC_STATE`                               | Second Activity line               | `Codex CLI`            |
| `process_poll_seconds` / `CODEX_RPC_PROCESS_POLL_SECONDS` | Process scan interval              | `1`                    |
| `retry_seconds` / `CODEX_RPC_RETRY_SECONDS`               | Discord reconnection interval      | `2`                    |
| `refresh_seconds` / `CODEX_RPC_REFRESH_SECONDS`           | Presence refresh interval           | `15`                   |
| `runtime_dir` / `CODEX_RPC_RUNTIME_DIR`                   | Explicit Discord socket directory   | automatic              |
| `lock_file` / `CODEX_RPC_LOCK_FILE`                       | Lock-file path                     | automatic              |

Rich Presence images can be configured with `large_image` and `large_text`
if the corresponding asset has been uploaded to the Discord Application.

## How it works

The monitor compares `/proc/<pid>/exe` with the Codex binary and reads the
launch time from `/proc/<pid>/stat`. When a process is detected, a Discord IPC
client starts and sends `SET_ACTIVITY` with `timestamps.start`. When the
process exits, the Activity is cleared. RPC errors do not stop Codex.

The project is designed for Linux and connects only to the local Discord IPC.
It is a single self-contained Go binary at runtime. The TOML parser is pinned
in `go.mod` and linked into the binary during the build.

## Uninstallation

First, stop and remove the service:

```bash
codex-rpc service uninstall
```

To completely remove the launcher, package, and user service, run this from the
cloned repository:

```bash
./scripts/uninstall.sh
```

The file-removal script refuses to continue while the user unit or one of its
known enable symlinks is present, and it never calls `systemctl`. It checks the
installer's marker and does not remove `~/.config/codex-discord-rpc`.

## Development and tests

```bash
gofmt -w cmd internal
go mod verify
go vet ./...
go test -race ./...
go test -cover ./...
CGO_ENABLED=0 go build -trimpath ./cmd/codex-rpc
bash -n scripts/*.sh
bash scripts/test-install.sh
bash scripts/check-public-repo.sh
systemd-analyze verify systemd/codex-discord-rpc.service
```

The utility version is printed with `codex-rpc --rpc-version`; `--version` is
passed through to Codex in wrapper mode. The current Codex CLI command
reference is available in the
[official documentation](https://developers.openai.com/codex/cli/reference/).
