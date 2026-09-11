# Contributing

## Local setup

The project intentionally has no runtime dependencies. Use Go 1.23 or newer
and run the checks from the repository root:

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

## Changes

- Keep the Discord IPC layer in the standard library. Review and pin every
  build-time dependency before adding it.
- Do not add Bot Tokens, Client Secrets, Application IDs, or personal paths to
  tracked files.
- Keep Linux-specific behavior explicit and test process/service changes with
  fakes where possible.
- Installation scripts must never invoke `systemctl`. Service lifecycle
  changes belong to explicit `codex-rpc service ...` commands.
- Keep the unit attached to `graphical-session.target`; it must never pull
  that target into a login session.
- Update `README.md` and `PLAN.md` when user-visible behavior changes.

Pull requests should explain the behavior change, tests run, and any manual
Discord or systemd verification performed.
