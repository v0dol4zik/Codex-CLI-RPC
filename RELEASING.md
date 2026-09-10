# Releasing

1. Update the version in `pyproject.toml` and
   `src/codex_discord_rpc/__init__.py`.
2. Run the same checks as CI:

   ```bash
   python3 -m unittest discover -s tests -v
   python3 -m compileall -q src bin
   bash -n scripts/install.sh scripts/uninstall.sh scripts/check-public-repo.sh
   bash scripts/check-public-repo.sh
   ```

3. Verify `codex-rpc --check`, process discovery, timer growth, Discord restart,
   and Presence cleanup on a Linux desktop.
4. Review `git diff`, commit the release, and push it.
5. Create an annotated tag such as `v0.1.0`, push the tag, and create a GitHub
   Release from that tag with a short changelog.

Never include a personal `config.toml`, Discord Client Secret, Bot Token, or
private path in a release archive.
