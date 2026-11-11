# rstack

Small agent skills and standalone developer tools. Tools work without an agent.

| Skill | Use it for |
|---|---|
| `blast-radius` | Before merging a change you don't fully trust: find what it breaks outside the diff and prove the one fact it's safe because of. |
| `unslop` | Cutting AI writing habits from PR descriptions, comments and docs. |
| `review` | Reviewing a PR you didn't write: intent, proven safety, tests, reader load, root causes and leftovers, with only proven findings. Uses `blast-radius` and `unslop`. |
| `machine-maintenance` | Inspecting disk pressure and reclaiming build caches or inactive worktrees without losing active work. |

## Tools

| Command | Purpose |
|---|---|
| `build-run` | Queue builds, share caches, clean scratch files and protect disk space. |
| `disk-report` | Inspect disk usage and growth since a saved scan. |
| `workspace-clean` | Audit Git worktrees; explicitly remove eligible inactive ones. |

Native binaries: [releases](https://github.com/rokewind/rstack/releases).
No Python or runtime packages. See [usage and installation](tools/README.md) and
[security boundaries](SECURITY.md).

## Install skills

```sh
mkdir -p ~/.claude/skills ~/.codex/skills
for s in skills/*/; do
  ln -sfn "$PWD/$s" ~/.claude/skills/"$(basename "$s")"
  ln -sfn "$PWD/$s" ~/.codex/skills/"$(basename "$s")"
done
```

Run it from the repo root. The skills are symlinked, so `git pull` updates them.

## License

MIT. Some skills are adapted from third-party work; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
