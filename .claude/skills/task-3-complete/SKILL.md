---
name: task-3-complete
description: >
  Close out one MVP phase task: re-run lint+test verify, dialectic, mark INDEX ✅,
  commit, push branch by default (opt out with --no-push), give manual test
  commands, checkout next stub-stem branch. Manual only — /task-3-complete TXX [--no-push].
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /task-3-complete — Close one MVP phase task

Run after `/task-2-execute TXX` when acceptance criteria pass. You **do not**
implement product features here — you re-verify, capture learnings, sync the
backlog, mark the INDEX complete, **commit**, **push** (unless `--no-push`),
give the user **manual test** commands, and **start the next task branch**.

Usually safe on a **smaller / cheaper** model (mechanical verify + dialectic
triage + git). If dialectic Mode A/B looks like a deep novel failure mode, you
may recommend the user re-run dialectic on a large model — do not block close-out
on that alone when verify is green and AC passed.

## Arguments

- Arguments: `T01` … `T10` (**all completed** — this skill is only used for future post-MVP tasks)
- Optional flag: **`--no-push`** — commit locally and continue, but **do not**
  `git push`
- If omitted: the task whose INDEX status is `InProgress`, else the most recently
  executed task in this conversation

Examples:

```text
/task-3-complete T04
/task-3-complete T04 --no-push
```

## Hard constraints

1. Write coding learnings only via `/dialectic-of-cognition` → `.cursor/rules/*.mdc`.
2. Do not invent downstream scope; only update later tasks when **this** task’s
   reality change would break or mislead their plans/AC.
3. INDEX completed status is **`✅`** — never write `Done` in the INDEX Status
   column. Task **files** may still use Status `Done` (or `✅`).
4. **Re-run the full lint + test (verify) suite** before marking complete or
   committing. Prefer **`make verify`**. Auto-fix first; remaining lint failures
   block close-out.
4a. **Verify tooling presence (hard abort):** Same as `/task-2-execute` — require
    root `Makefile` with `verify` (lint+test), stack lint config (Go: `.golangci.yml`),
    and `lefthook.yml` (pre-commit → verify). If missing: **stop** — do not mark ✅,
    do not commit, do not push. Tell the user to restore verify tooling first.
    Never treat `go test ./...` alone as the complete gate.
5. Commit is **part of this skill** (user invoked `/task-3-complete`). Still:
   - Never update git config
   - Never `--trailer` / Co-authored-by
   - Never force-push
   - Do not commit secrets (`.env`, credentials, API keys)
   - Prefer committing only when pre-commit (lefthook) would also pass
6. **Push by default** after a successful commit (`git push -u origin HEAD`
   when upstream is unset). Skip push only if **`--no-push`** was provided.
7. Close-out **must** include a **Manual test** section (commands + what to
   look for), or **`Nothing to test`** with a clear reason.

## Branch naming

- Branch = stub **filename** without `.md` (no folder path): `<stub-stem>`
  - Example current: `T01-scaffold-config-slog` (not `planning/phases/T01-scaffold-config-slog`)
  - Example next: `T02-secrets-gateway-key` (from **Next** task’s stub file)
- Do **not** use `task-TXX` / `task-TYY`.
- Do **not** prefix with `planning/phases/`.

If not already on the current task’s stub-stem branch, warn and either
checkout/create it or ask — do not commit on `main`/`master` by surprise.

## Procedure

### 1. Preconditions

- Read `planning/phases/INDEX.md` and the target task file
- Confirm Acceptance criteria are checked (or Status is already complete)
- If AC failed / Blocked: stop — do not mark ✅, do not commit, do not push
- Note whether `--no-push` is set

### 2. Verify gate (mandatory)

**Presence check first** (fail closed):

```bash
test -f Makefile && grep -q '^verify' Makefile
test -f lefthook.yml
test -f .golangci.yml   # Go
make verify
```

If tooling is missing: stop — do not dialectic-mark-complete, commit, or push.
Re-run **`make verify`**. If it fails: stop — send back to execute/fix.

Record results in the task **Verification** section if not already current.

### 3. Dialectic (mandatory)

Read and **fully execute** `.claude/skills/dialectic-of-cognition/SKILL.md`
(Modes A and B as applicable, then **Mode C** triage for Turboplan process
feedback — almost always a skip). Summarize the skill’s output in your close-out.

### 4. Downstream task sync

Scan **Next** and later tasks whose notes/AC assume the pre-change world.
Update with a short **Reality note** only — do not expand scope or mark them complete.
If nothing stale: say so.

### 5. Mark complete

1. Task file: Status → `Done` (or `✅`); fill **Learnings**; Verification / Files touched;
   fill **Manual test (for humans)** (commands + what to look for, or
   `Nothing to test — <why>`)
2. `planning/phases/INDEX.md`: set that row’s Status to **`✅`**
3. Status History rows on task file (+ INDEX notes if used)
4. Confirm Depends-on consumers still make sense

### 6. Commit on current stub-stem branch (mandatory)

```bash
git add -A
git commit -m "$(cat <<'EOF'
TXX Short description of what this task delivered.

EOF
)"
```

Message style: `TXX <short description>`. No `--trailer`.
Pre-commit hooks (lefthook) should run verify; if the hook fails, fix and create
a **new** commit — do not `--no-verify` unless the user explicitly overrides.

### 7. Push (default)

Unless **`--no-push`**:

```bash
git push -u origin HEAD
```

If `origin` is missing, say so, skip push, and tell the user how to add a remote
— do not invent remotes. Never `--force`.

### 8. Manual test handoff (mandatory)

Write commands the **human** can run to manually exercise this task’s change.
Be concrete for this Go CLI/gateway (e.g. `go run . …`,
`make run`, `curl` against local/gateway URL, doctor output). Include:

- How to start / invoke
- Example request or CLI path
- What success looks like

If not applicable (pure docs/rules, library-only with no runnable surface yet,
depends on a later tunnel/CLI task):

```text
Nothing to test — <one-sentence why>
```

### 9. Checkout next task branch (mandatory)

```bash
# stub-stem = Next task file name without .md (e.g. T02-secrets-gateway-key)
git checkout -b <next-stub-stem>
# or: git checkout <next-stub-stem> if exists
```

Do **not** start implementing TYY in this skill.

### 10. Output format

```
## Completed — TXX Title

### Verify
- lint: …
- tests: …

### Dialectic
…

### Downstream updates
- none / …

### INDEX
TXX → ✅

### Git
- Branch: <stub-stem>
- Commit: <short sha> <message>
- Push: pushed to origin | skipped (--no-push) | skipped (no origin): …
- Now on: <next-stub-stem>

### Manual test
<commands + what to look for>
# or: Nothing to test — <why>

### Next
/task-1-plan TYY
```

## Do not

- Re-implement the task or start the next task’s code
- Write `Done` into the INDEX Status column
- Skip dialectic when debugging triggers fired
- Skip lint+test verify
- Skip the commit + next-branch steps when AC passed
- Use `--no-verify` / skip hooks without explicit user approval
- Skip the default push when `--no-push` was **not** set and `origin` exists
- Force-push
- Omit Manual test / Nothing to test
