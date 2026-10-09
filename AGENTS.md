# pigo

Read `ARCH.md` first. Obey section 11 (rules for LLM agents working on this repo).

## Commits

Commit after every completed change. Commit intelligently, not mechanically.

- **Commit only working states.** Run `gofmt -l .`, `go vet ./...`, and `go test ./...` first. Never commit a tree that fails build or tests.
- **Group by intent.** One coherent change per commit. Do not mix unrelated edits; split them into separate commits.
- **Stage precisely.** `git add <paths>`, then `git diff --cached` to review. Avoid blind `git add -A`.
- **Write a real message.** Imperative subject under ~72 chars, body only if the "why" is not obvious. Example: `tools/edit: reject ambiguous old string`.
- **Never commit** secrets, `auth.json`, `.env*`, keys, `features.md`, the `pigo` binary, or other `.gitignore` entries. If a needed file is ignored, fix `.gitignore` instead of force-adding.
- **No commit for no-ops.** If a change produces no diff, say so and stop.
- **Don't rewrite history** you did not create. No `--amend`, rebase, or force-push unless the user asks.
