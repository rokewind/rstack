# Standalone tools

Download the binary for your OS/architecture and `SHA256SUMS` from
[releases](https://github.com/rokewind/rstack/releases). Verify its checksum before
running it, rename it to `rstack`, then:

```sh
chmod +x rstack
./rstack install
export PATH="$HOME/.local/bin:$PATH"
```

Installs into `~/.local/bin`, backs up replaced files, and needs no sudo or runtime.
Optional: `./rstack install --shims --maintenance` wraps existing Go/Cargo/npm/pnpm/
Terraform build commands and enables idle cleanup every minute on macOS. It does
not edit shell configuration. The installer preserves existing guard settings.

```sh
build-run -- go test ./...
build-run -- sh -c 'go build -o "$BUILD_OUT/app" . && cp "$BUILD_OUT/app" ./app'
build-run --status
disk-report --save ~/repos ~/.cache   # first baseline; repeat to compare/update
disk-report --json ~/repos ~/.cache
workspace-clean --repo ~/repos/project --base origin/main
workspace-clean --repo ~/repos/project --base origin/main \
  --remove /exact/worktree/path --confirm-inactive --apply
pdf-render --png ./pages report.html     # writes report.pdf and page PNGs
```

**Builds:** one shared slot, no FIFO guarantee; nested commands reuse the slot.
Start at 10 GiB free, stop below 7 GiB to target a 5 GiB reserve. Exit 75 means a
guard error or insufficient space. Scratch (`BUILD_OUT`, `TMPDIR`, `GOTMPDIR`) is
deleted on exit; copy wanted artifacts out. Go/npm/uv caches are shared and cleared
when over 8 GiB between builds. Go modules and Terraform providers are shared but
not pruned. Config: `~/.config/build-guard.json`; state: `~/.local/state/build-guard`.
Never wrap Terraform apply/destroy/state operations or detached/background jobs.
`build-run --maintain` performs idle cleanup; a surviving orphan group blocks it.

**Disk reports:** immediate children of the selected directories, sorted by allocated
bytes. Symlinks aren't followed. `--save` updates the baseline only after a complete
scan; baseline data stays in `~/.local/state/rstack/disk-report`. APFS clones and
overlapping roots can overstate recoverable space. No automatic scheduled scans.

**Worktrees:** audit-only by default. Removal requires an exact path, inactivity
confirmation, merged commits and no tracked/untracked/ignored changes, locks or
open files. Branches survive. Git refs are local: fetch yourself first if needed.
Requires Git and `lsof`; active/pinned agent sessions cannot be inferred reliably.
No automatic artifact-folder or worktree deletion, and no `--force` option.

**PDFs:** renders a local HTML file with an installed Chrome-family browser
(`PDF_RENDER_CHROME` overrides the choice) using a throwaway profile, no default
headers or footers, and a time limit (`--timeout`, default 60s) that kills the
browser's process group. Page size, margins and footers come from CSS `@page`.
The PDF is written beside the destination and renamed into place only when it is
valid. `--png` needs `pdftoppm` (poppler) and replaces that document's earlier
page images. The HTML can load network resources like any web page.

Build from source with Go 1.25+ (standard library only):
`cd tools/rstack && go build -trimpath -o rstack .`.
Tests: `go test -race ./...`. Set `RSTACK_TEST_BINARY` to a compiled binary named
`rstack` to include installer checks. `sh tools/build-release.sh` builds all release
targets and checksums from the repo root. macOS/Linux only.
