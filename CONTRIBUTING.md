# Contributing

## Local setup

The project intentionally has no runtime dependencies. Use Python 3.11 or
newer and run the checks from the repository root:

```bash
python3 -m unittest discover -s tests -v
python3 -m compileall -q src bin
bash -n scripts/install.sh scripts/uninstall.sh scripts/check-public-repo.sh
bash scripts/check-public-repo.sh
```

If `ruff` is available, run `ruff check src tests` before opening a pull
request.

## Changes

- Keep the Discord IPC layer dependency-free.
- Do not add Bot Tokens, Client Secrets, Application IDs, or personal paths to
  tracked files.
- Keep Linux-specific behavior explicit and test process/service changes with
  fakes where possible.
- Update `README.md` and `PLAN.md` when user-visible behavior changes.

Pull requests should explain the behavior change, tests run, and any manual
Discord or systemd verification performed.
