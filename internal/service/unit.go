package service

const Unit = `# Managed by Codex-CLI-RPC
[Unit]
Description=Discord Rich Presence monitor for Codex CLI
Documentation=https://github.com/v0dol4zik/Codex-CLI-RPC
After=graphical-session.target
PartOf=graphical-session.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=exec
ExecStart=%h/.local/bin/codex-rpc --monitor
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Restart=on-failure
RestartPreventExitStatus=2 75 127 203
RestartSec=5s
TimeoutStopSec=5s
UMask=0077
MemoryMax=64M
TasksMax=64
NoNewPrivileges=yes
RestrictAddressFamilies=AF_UNIX

[Install]
WantedBy=graphical-session.target
`

// These exact historical units are accepted only so an installation created
// by v0.1.x can be atomically upgraded. Modified lookalikes are never treated
// as files owned by this project.
const legacyGraphicalUnit = `[Unit]
Description=Discord Rich Presence monitor for Codex CLI
After=graphical-session.target
PartOf=graphical-session.target

[Service]
Type=simple
ExecStart=%h/.local/bin/codex-rpc --monitor
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Restart=always
RestartSec=2
TimeoutStopSec=5

[Install]
WantedBy=graphical-session.target
`

const legacyDefaultUnit = `[Unit]
Description=Discord Rich Presence monitor for Codex CLI
After=graphical-session.target
Wants=graphical-session.target

[Service]
Type=simple
ExecStart=%h/.local/bin/codex-rpc --monitor
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Restart=always
RestartSec=2
TimeoutStopSec=5

[Install]
WantedBy=default.target
`
