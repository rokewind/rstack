# rstack

Small agent skills for Claude Code and Codex.

| Skill | Use it for |
|---|---|
| `blast-radius` | Before merging a change you don't fully trust: find what it breaks outside the diff and prove the one fact it's safe because of. |
| `unslop` | Cutting AI writing habits from PR descriptions, comments and docs. |

## Install

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
