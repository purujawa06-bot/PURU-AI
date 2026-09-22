---
name: puru-verify
description: Verify Puru bootstrap results via git status, git diff, gofmt, vet, and tests. Use after a puru-bootstrap round or when checking whether Puru introduced bugs.
---

# Puru Verify

Verify a Puru self-improvement round: inspect what changed, fix breakage, prove no bugs.

## 1. Inspect

```bash
git status --short
git diff --stat
git diff -- <files>
```

Read `SELF_IMPROVE.md` for the round notes. Known failure mode: Puru calls a method that does not exist (e.g. `res.String()` on `execResult` in `internal/ai/tools.go`) — check the diff for calls to undefined methods/fields and fix by reverting to the prior working pattern (e.g. the `success/exit_code/output` map, consistent with the local `errVal` helper).

## 2. Validate

```bash
gofmt -l .
go vet ./...
go test ./... -count=1
```

All must be clean. For a quick targeted check: `go test ./internal/ai/ -count=1 -run 'TestPicoclaw|TestExec|TestClamp' -v`.

Watch for: `argStr` TrimSpace applied to `content`/`old_text`/`new_text` args (would corrupt file bytes — those handlers must keep raw `args[...].(string)`); `list_dir` format changes breaking `TestPicoclawStyleResponses` (`Contains "FILE: w.txt"`); truncation markers referenced by tests.

## 3. Report

Summarize in Bahasa Indonesia, concisely: what Puru changed (files), any bug found + fix, verification results (`gofmt`/`vet`/`test`), and current `git status`. Note that changes are uncommitted. NEVER commit/push unless the user explicitly asks — per AGENTS.md that requires an annotated tag `vX.Y.Z` plus `git push origin main --follow-tags`.
