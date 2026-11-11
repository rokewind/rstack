---
name: machine-maintenance
description: "Inspect disk pressure, build caches and stale Git worktrees with rstack tools; reclaim space when requested while preserving active work. Use for disk-space checks and development-machine cleanup, not code or documentation maintenance."
---

# Machine maintenance

Use `disk-report`, `workspace-clean` and `build-run` on the machine the user names.
An inspection request authorizes inspection, not deletion. Keep cleanup within the
user's stated scope; reuse existing authorization rather than asking again.

## Inspect

- Check the tools are available on the target machine. If missing, consult
  [installation and usage](../../tools/README.md); do not substitute improvised
  deletion commands. Use an existing authorized remote connection when applicable.
- Record `df -h <path>` for the affected filesystem and `build-run --status`.
  Start `disk-report --json <roots...>` with the relevant workspace/cache roots;
  narrow into large directories rather than repeatedly scanning the whole home.
- Growth requires a saved baseline for the same roots. No baseline means current
  usage only. Use `--save` to establish or update one after inspecting the report.
  Incomplete scans are not evidence that space is unused; APFS allocation figures
  are estimates, not guaranteed reclaimable bytes.

## Reclaim

For authorized cache cleanup, run `build-run --maintain`. It skips active builds
and limits deletion to its managed scratch and caches. A successful exit alone
does not prove anything was removed. Do not stop another thread's build or weaken
the guard to make cleanup proceed.

For worktrees, choose the intended integration ref and audit first:

```sh
workspace-clean --repo /path/to/repo --base origin/main --json
```

The merge check uses local refs; establish their freshness before relying on it.
Use returned worktree paths, not guessed directory names. Treat every hold reason
as a refusal. A clean, merged worktree may still belong to a pinned or active
session: establish inactivity from the user or reliable session evidence, including
background jobs. Missing process activity alone is insufficient.

When the exact candidate is authorized and confirmed inactive:

```sh
workspace-clean --repo /path/to/repo --base origin/main \
  --remove /exact/returned/path --confirm-inactive --apply
```

If authorization or inactivity is unclear, present the candidate and the missing
fact. If a check refuses removal, retain it; never bypass with force or `rm -rf`.
Untracked and ignored files are user data too. VM disks, Terraform state/plans,
receipts, credentials and unrelated caches are outside this workflow's cleanup.
The guard is not a sandbox; see [security boundaries](../../SECURITY.md).

## Verify and report

Recheck free space on the same filesystem and re-audit affected worktrees. Report
what was removed, what was retained and why, and the observed space change. State
when nothing changed. Stop when the requested outcome is met; do not keep deleting
to maximize free space or add schedules/configuration changes unless requested.
