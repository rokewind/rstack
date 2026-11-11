# build-guard

One build at a time, shared caches, automatic scratch cleanup. No dependencies.

```sh
python3 install.py                       # installs ~/.local/bin/build-run
export PATH="$HOME/.local/bin:$PATH"
build-run -- go test ./...
build-run -- sh -c 'go build -o "$BUILD_OUT/app" . && cp "$BUILD_OUT/app" ./app'
build-run --status
```

Optional installer flags: `--shims` wraps installed Go, Cargo, npm, pnpm and
Terraform build commands; `--maintenance` enables minute-by-minute idle cleanup
on macOS. Existing commands are backed up. Shell config is never edited.

- Competing builds wait on a file lock; nested builds reuse the slot. No FIFO guarantee.
- New builds require **10 GiB** free; builds stop below **7 GiB**, targeting a
  **5 GiB** reserve. Exit code 75 indicates disk pressure or a guard error.
- `$BUILD_OUT`, `$TMPDIR` and `$GOTMPDIR` are deleted after each run. Copy final
  artifacts out before exit. Explicit outputs elsewhere are retained.
- Go/npm/uv caches are shared. Idle cleanup clears managed caches over 8 GiB.
  Go modules and Terraform providers are shared but not automatically deleted.
- Source, Terraform state, plans and receipts are never cleaned. Never wrap
  Terraform apply/destroy/state operations or detached/background jobs.

Configuration: `~/.config/build-guard.json`; state: `~/.local/state/build-guard/`.
Run `build-run --maintain` for idle cleanup. A surviving orphan build blocks
cleanup: inspect `active.json` and its process group; don't delete the lock file.

This is a polling guard, not a disk quota. Unmanaged processes, absolute commands
that bypass wrappers, detached jobs and container daemons can still fill the disk.
Existing builds are not retroactively managed. Tests: `python3 -m unittest -v`.
