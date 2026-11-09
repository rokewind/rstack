---
name: review
description: "Review a pull request or branch diff you did not write. States the intent, proves the one fact the change is safe because of, checks tests, reader load, root causes and leftovers, and reports only findings a command proves. Use for /review, 'review PR N', or before merging."
---

# Review

Review a change you did not write. Report findings; do not fix them.

## Scope

The scope is what the user names: a PR number, a branch or a path. Otherwise it is the current branch's diff from its merge base with the default branch. Record the full head SHA you review. If the head moves during the review, start again on the new head.

Read the diff, the PR description, the linked issue and the code around each change, not just the hunks.

## Steps

1. **Intent.** Write one sentence on what the change is for, from the PR description, issue and commits. Judge every finding against it. If you cannot tell, say so and stop.
2. **Safety.** Run [blast-radius](../blast-radius/SKILL.md): find the one fact the change is safe because of, look where search can't see, and prove the fact by running real code.
3. **Tests.** Apply the two edits in [test behavior, not implementation](references/test-behavior-not-implementation.md) to each added or changed test. Flag tests that pass when the behavior breaks or fail when only the internals change. Flag changed behavior that no test would catch breaking.
4. **Reader load.** Apply [minimize reader load](references/minimize-reader-load.md) to each new layer, wrapper, adapter, flag or piece of state. Flag ones with one caller, one implementation or a pass-through shape.
5. **Root cause.** For a bug fix, apply [fix root causes](references/fix-root-causes.md). Flag a guard where the symptom surfaced when the state should never exist, and search for the same fault elsewhere.
6. **Leftovers.** When the change adds a new way to do something, apply [migrate callers, then delete legacy APIs](references/migrate-callers-then-delete-legacy-apis.md). Flag the old way if it survives without a stated reason.

## Evidence

A finding counts only if you ran something that shows it: a failing test, a repro script, or a command and its output. Leave out anything you did not run, including style, naming and risks that need a future change to matter. Keep repro scripts out of the repository unless the author should turn them into tests. No findings is a valid result.

A finding is blocking when it breaks the intent, loses data, widens access, or leaves changed behavior without a test that would catch it breaking. Everything else is non-blocking.

## Report

Write it through [unslop](../unslop/SKILL.md) and keep it short.

- **Head:** the full SHA reviewed.
- **Intent:** the one sentence.
- **Safe because:** the fact, how far it was proven and the proof.
- **Findings:** each with severity, `file:line`, the command, what it printed and what the intent requires.
- **Cleared:** what you checked and found fine, one line each.
- **Verdict:** `ready` when no blocking findings remain, otherwise `changes-required`, with the blocking count.

When asked to post it, post the report as one PR comment. On a later push, recheck the new head and say how each earlier finding was resolved.
