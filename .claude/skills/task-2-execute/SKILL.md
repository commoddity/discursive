---
name: task-2-execute
description: >
  Execute one Planned/Pending MVP phase task from planning/. Implement
  until acceptance criteria pass, record verification, then hand off to
  /task-3-complete. Manual only — /task-2-execute TXX.
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /task-2-execute — Execute one atomic MVP task

You implement **exactly one** task from `planning/`. Stop when Acceptance
criteria pass (or blocked). Closing the task (dialectic, INDEX ✅, downstream
sync, commit, push, Manual test) is **`/task-3-complete`**.

This skill is intended to run on a **smaller / faster / cheaper** model when the
Execution plan from `/task-1-plan` meets the handoff bar. Follow the plan
literally; if the plan is too vague to proceed, **stop** and send back to
`/task-1-plan` (or ask the user to re-plan on a large model) — do not invent a
new design. If the plan marks **`Execute model recommendation: large`**, tell
the user before heavy work if they are still on a small model.

## Arguments

- Task id: `T01` … `T10`
- If omitted: first INDEX `Planned`, else first actionable `Pending`

## Hard constraints

1. Obey **Karpathy Behavioral Guidelines** in `.cursor/rules/general.mdc` in full
   (Think Before Coding, Simplicity First, Surgical Changes, Goal-Driven Execution).
   Do not treat “surgical” as the only rule.
2. Host Go CLI/daemon; macOS/Linux; Go 1.26.5+.
3. OpenAI-compat gateway routes by **alias→provider→model** to Moonshot Kimi
   **and** DeepSeek (`gateway.mdc` / `kimi.mdc` / `deepseek.mdc`).
4. Upstream keys (Moonshot + DeepSeek) never in Cursor; gateway `sk-{alnum}` only;
   never log API keys. `log/slog` for product logging.
5. Never vendor `examples/use-kimi-on-cursor` or name Claude Code in product source.
6. Read matching domain rules before coding (`gateway`, `kimi`, `deepseek`, `tunnel`,
   `cobra`, etc.).
7. **Table-driven `go test`** — do not mark Acceptance criteria passed for new/changed
   Go package behavior without table-driven tests and a passing `go test` on touched
   packages (not only `go build`). Live-only tasks may use `-tags=live` plus unit tables
   where applicable.
8. **Run the full lint + test (verify) suite** before claiming AC passed.
   Prefer **`make verify`**. Auto-fix first; fail only on remaining unfixable issues.
9. **Verify tooling presence (hard abort):** Before claiming AC passed, confirm:
   - Root `Makefile` with a `verify` target that runs **lint + test** (+ build when applicable)
   - Lint config when the stack expects it (Go: `.golangci.yml`)
   - `lefthook.yml` (or approved equivalent) with **pre-commit → that verify**
   If any are missing: **stop**, Status `Blocked` (or refuse handoff), tell the user to
   restore verify tooling — do **not** treat `go test ./...` alone as “lint+test green.”
10. Prefer **latest stable** versions when adding new modules/tools (runtime floor
   remains Go 1.26.5+; document pins if upgrade is blocked).
11. Do not commit unless the user explicitly asked (`/task-3-complete` commits and
    **pushes by default** on close-out; `--no-push` to skip push).
12. Only one phase task `InProgress` unless human approved parallel work.

## Procedure

### 0. Preconditions

- Read task + `planning/INDEX.md`
- Depends-on is `✅` / `Done` / `—`
- If no Execution plan section, run `/task-1-plan` first

### 1. Status → `InProgress` (task file + INDEX)

Log Status History.

### 2. Implement

- Follow Execution plan
- Add/update tests with the code
- Go: `go build ./...` / `go test` for touched packages; then full **`make verify`**
  (presence check first — see Verify gate)
- New/changed Go behavior: table-driven tests (`[]struct` + `t.Run`) covering success + edges
- Prefer parity tests inspired by `examples/use-kimi-on-cursor/src-tauri/tests/`
- Auto-fix / lint continuously when practical

### 3. Verify gate (mandatory)

**Presence check first** (fail closed):

```bash
test -f Makefile && grep -q '^verify' Makefile
test -f lefthook.yml   # or documented equivalent
test -f .golangci.yml  # Go projects
make verify
```

If `Makefile` / `verify` / `lefthook.yml` (or lint config for this stack) is missing:
stop — Status `Blocked`; do **not** hand off as passed with only `go test`.

Run **`make verify`** (lint + tests + build). Record commands and outcomes in
**Verification**. If verify fails: fix or set Status `Blocked`.

### 4. Acceptance → all checked or Status `Blocked`

AC must include: tests present for new code; lint+test suite green.

### 5. Record on the task file

Fill Verification and Files touched. Leave INDEX status as `InProgress` until
`/task-3-complete`.

### 6. Hand off (mandatory)

Tell the user:

> Run `/task-3-complete TXX` — re-verify → dialectic → INDEX → ✅ → commit → push (default; `--no-push` to skip) → Manual test handoff → next branch.

If the user already asked to execute **and** complete in one go, run
`/task-3-complete` yourself now.

### 7. Output format

```
## Executed — TXX Title
### Result
Acceptance passed / Blocked
### Verification
- lint: …
- tests: …
- …
### Files touched
- …
### Next
/task-3-complete TXX
```

## Do not

- Execute two phase tasks in one invocation
- Mark INDEX `✅` without `/task-3-complete`
- Write `Done` into the INDEX Status column
- Skip tests or skip the lint+test verify gate
- Commit unless asked (`/task-3-complete` commits and pushes by default)
- Invent design that `/task-1-plan` left unspecified — re-plan instead
