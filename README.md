# Codex-cli Discord RPC

```ascii
_________     _________                _________________________
__  ____/___________  /________  __    ___  __ \__  __ \_  ____/
_  /    _  __ \  __  /_  _ \_  |/_/    __  /_/ /_  /_/ /  /
/ /___  / /_/ / /_/ / /  __/_>  <      _  _, _/_  ____// /___
\____/  \____/\__,_/  \___//_/|_|      /_/ |_| /_/     \____/
```

[![AI Slop Inside](https://sladge.net/badge.svg)](https://sladge.net)
[![Platform: Linux](https://img.shields.io/badge/platform-Linux-FCC624?logo=linux&logoColor=black)](#requirements)
[![Python 3.11+](https://img.shields.io/badge/Python-3.11%2B-3776AB?logo=python&logoColor=white)](https://www.python.org/)
[![Runtime dependencies: none](https://img.shields.io/badge/runtime_dependencies-none-2ea44f)](pyproject.toml)
[![Service: systemd](https://img.shields.io/badge/service-systemd-0086CC?logo=systemd&logoColor=white)](systemd/codex-discord-rpc.service)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**A Python script for Linux that shows how long Codex CLI has been running in Discord Rich Presence.**

After installation, a user-level systemd service monitors `codex` processes
and displays an Activity in Discord.

## Features

- the timer starts when `codex` is actually launched and resets when it closes;
- closing the Discord client does not reset the timer;
- a lock file prevents a second RPC client from starting;
- if several `codex` processes are running, the newest one is selected;
- Codex continues working when Discord is unavailable;
- no Bot Token, external server, or OpenAI API key is used.

## Requirements

- a Linux distribution that supports the Discord desktop client;
- Python 3.11 or newer;
- Codex CLI, with the `codex` command available in `PATH`;
- a Discord Application ID from the Developer Portal.

## Installation

```bash
git clone <repository-URL> codex-discord-rpc
cd codex-discord-rpc

mkdir -p ~/.config/codex-discord-rpc
cp config.example.toml ~/.config/codex-discord-rpc/config.toml
$EDITOR ~/.config/codex-discord-rpc/config.toml

./scripts/install.sh
```

Get a Discord Application ID and replace the value in `config.toml` with your
own. For the background systemd service, the ID must be stored in the config
file, not only in a variable in your shell environment.

The installer places the launcher in `~/.local/bin/codex-rpc`, copies the
package to `~/.local/share/codex-discord-rpc`, and enables:

```text
~/.config/systemd/user/codex-discord-rpc.service
```

## Running and managing

After installation, you can start Codex and check the RPC:

```bash
codex
```

Check the RPC and systemd service:

```bash
codex-rpc --check
codex-rpc service status
```

Manage the user service:

```bash
codex-rpc service install
codex-rpc service restart
codex-rpc service stop
codex-rpc service uninstall
```

If the systemd service is unavailable, you can start the monitor manually:

```bash
codex-rpc --monitor
```

Do not add `alias codex='codex-rpc'` while the background service is enabled:
this would create a second RPC client. Wrapper mode is kept for manual launches
and compatibility:

```bash
codex-rpc exec "check the tests"
```

## Configuration

The script's main settings are stored in
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
| `lock_file` / `CODEX_RPC_LOCK_FILE`                       | Lock-file path                     | automatic              |

Rich Presence images can be configured with `large_image` and `large_text`
if the corresponding asset has been uploaded to the Discord Application.

## How it works

The monitor compares `/proc/<pid>/exe` with the Codex binary and reads the
launch time from `/proc/<pid>/stat`. When a process is detected, a Discord IPC
client starts and sends `SET_ACTIVITY` with `timestamps.start`. When the
process exits, the Activity is cleared. RPC errors do not stop Codex.

The project is designed for Linux and connects only to the local Discord IPC.

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

The script checks the installer's marker before removing the installation
directory. It does not remove the user configuration at
`~/.config/codex-discord-rpc`. The `codex-rpc service uninstall` command only
removes the user service.

## Development and tests

```bash
python3 -m unittest discover -s tests -v
python3 -m compileall -q src bin
bash -n scripts/install.sh scripts/uninstall.sh scripts/check-public-repo.sh
bash scripts/check-public-repo.sh
```

The current Codex CLI command reference is available in the
[official documentation](https://developers.openai.com/codex/cli/reference/).
