---
name: no-comments
description: "Write code without comments, and remove the comments already in code you touch. Keeps only directives a tool reads. Use whenever writing or editing code, or when asked to strip comments."
---

# No comments

Code carries no comments. Every comment is a line someone has to read, keep true and step over, and most restate the code or go stale.

## Writing code

- Write none: no doc comments, no inline notes, no section banners, no commented-out code, no TODOs.
- Say it in the code instead. Name things for what they do, extract a named function, use a type or a constant, write a test that shows the behaviour.
- Put the reason for a change in the commit message. Put how to use something in the README or the docs the project already has. Put open work in the issue tracker.

## Removing comments

When asked, or in code you are already changing, delete comments with these exceptions, which tools read:

- Build and compiler directives, such as `//go:build`, `//go:embed`, `//go:generate`, `// +build`, `#pragma`, `"use strict"`.
- Shebang lines.
- Linter and type-checker controls the project relies on, such as `// eslint-disable-next-line`, `# noqa`, `// @ts-expect-error`, `#[allow(...)]`, `# type: ignore`.
- License headers the project's license requires in each file.
- Comments inside string literals or generated files; change generated files through their generator.

Remove a comment's line when it was alone on it, and keep the surrounding formatting. Run the formatter and the tests afterwards.

If a comment holds something nobody can recover from the code, such as why an odd-looking workaround exists or a link to the bug it guards against, move that to the commit message or the project's docs before deleting it. Do not leave it in place.

## Checking

List added comments in a diff before calling a change finished. For example, for Go:

```sh
git diff --cached -U0 -- '*.go' | grep -E '^\+\s*//' | grep -vE '^\+\s*//go:'
```

Adapt the pattern to the language's comment syntax. No output means no comments were added.
