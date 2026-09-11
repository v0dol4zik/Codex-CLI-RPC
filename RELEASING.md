# Releasing

1. Update the version in `internal/version/version.go`.
2. Run the same checks as CI:

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

3. Review the unit and installer diff. Confirm that file installation never
   calls `systemctl` and the unit never pulls `graphical-session.target`.
4. In an isolated desktop test account, verify `codex-rpc --check`, process
   discovery, timer growth, Discord restart, Presence cleanup, and explicit
   service lifecycle commands.
5. Review `git diff`, commit the release, and push it.
6. Create an annotated tag such as `v0.2.0`, push the tag, and create a GitHub
   Release from that tag with a short changelog.

Never include a personal `config.toml`, Discord Client Secret, Bot Token, or
private path in a release archive.
