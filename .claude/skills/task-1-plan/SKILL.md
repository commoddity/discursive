---
name: task-1-plan
description: >
  Plan or re-plan a single MVP phase task from planning/phases/. Produce a
  handoff-ready plan so a lesser/cheaper model can run /task-2-execute. Prefer
  a large model for this skill. Manual only — /task-1-plan TXX.
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /task-1-plan — Plan one atomic MVP task

You refine **one** task file under `planning/phases/` so a **lesser / cheaper
agent** can run `/task-2-execute` next without rediscovering the design. Prefer
running this skill on a **large / expensive** model. You do **not** implement
product code unless the user explicitly asks.

## Arguments

- Task id: `T01` … `T10` (or path like `planning/phases/T04-sanitizer.md`)
- If omitted: read `planning/phases/INDEX.md`, pick the first `Pending`/`Planned`
  task whose Depends-on is `✅` / `Done` / `—`.

## Hard constraints (never violate in the plan)

1. Follow `.cursor/rules/general.mdc` — including **Karpathy Behavioral Guidelines**
   (think / simplicity / surgical / goal-driven) and matching domain spokes.
2. **Host Go CLI/daemon** for macOS/Linux (Go 1.26.5+) — not Docker-primary, not Wails/Vue/Tauri.
3. **OpenAI-compat gateway** routing by alias→provider→model to **Moonshot Kimi**
   and **DeepSeek** (see `gateway.mdc` / `kimi.mdc` / `deepseek.mdc`).
4. **Key security** — Moonshot + DeepSeek keys never in Cursor; gateway `sk-{alnum}` only.
5. **Public HTTPS** for Cursor Base URL (Quick Tunnel or BYO) — never localhost as the Cursor URL.
6. **Logging** — `log/slog` only for product logs.
7. **Inspiration** — study `examples/use-kimi-on-cursor/` → reimplement in Go; never vendor.
8. **Agent-safe aliases** — full primary table in `gateway.mdc` (Kimi + DeepSeek;
   five aliases). Do not invent a Moonshot-only map.
9. Sequence of record: `planning/phases/INDEX.md`.
10. Stay inside this task’s Acceptance Criteria — no gold plating (Simplicity First).
11. Execution plan steps must use `→ verify:` pairs (Goal-Driven Execution).
12. **Handoff fidelity:** the plan must be technically detailed enough for a
    smaller execute model (see hub “Model split” / delivery notes). Vague plans
    are not done.
13. Prefer verification commands already used in the hub (`make verify`,
    `go test`, `go build`). Plans must include **lint + test** gates.
14. **Table-driven Go tests** — for any task that adds/changes Go behavior, the
    Execution plan **must** include table-driven test steps (`[]struct` + `t.Run`)
    and a `go test` verify for touched packages (not only `go build`).
15. Prefer **latest stable** module/tool versions when adding new deps (document
    pins if the environment cannot upgrade; runtime floor remains Go 1.26.5+).
16. If the task is ambiguous, stop and ask (Think Before Coding) — do not silently pick.
17. If this task is *exceptionally* hard even with a detailed plan, add
    **`Execute model recommendation: large`** with a one-line why; otherwise omit
    (default = small/cheap execute is fine).

## Procedure

### 1. Load context

- Read `planning/phases/INDEX.md`
- Read the target task file end-to-end
- Read depends-on task’s **Learnings** / **Verification** if `✅` / `Done`
- Skim `.cursor/rules/general.mdc` + matching domain rules (`kimi`, `deepseek`,
  `gateway`, `tunnel`, `cursor-settings`, `usage`, `unix-packaging`, `go`, `cobra`)
- For sanitizer/proxy work: consult reference tests under
  `examples/use-kimi-on-cursor/src-tauri/tests/` and Kimi docs via `kimi.mdc`
- Inspect the **current** repo tree

### 2. Reality check

List what exists vs what the task assumes. Update Implementation notes / AC if
the codebase diverged. Record **Reality notes** if upstream `/task-3-complete`
left any.

### 3. Write execution plan into the task file

Write for a **junior / smaller-model executor**: no implied context.

```markdown
## Execution plan (filled by /task-1-plan)

**Date:** YYYY-MM-DD
**Codebase snapshot:** …
**Execute model:** small/default | large (only if justified below)

### Context for executor
- Goal in one paragraph
- Key files/packages involved (paths)
- Invariants from rules that apply (providers, keys, slog, aliases, …)

### Steps
1. … (concrete edit/create) → verify: …
2. … → verify: …
   (Go behavior: include table-driven tests + `go test ./…` verify)

### Tests to add
- Table cases / scenarios …

### Verify commands
- `go test ./…` / `make verify` / …

### Risks / pitfalls
- …

### Out of scope
- …

### Execute model recommendation
- default (small/cheap) | large — rationale: …
```

Set Status to `Planned` only when a lesser agent could follow the plan cold.
Append Status History row.

### 4. Output to user

Ready? / Key steps / AC / Execute model: default|large / Next: `/task-2-execute TXX`

## Do not

- Implement the task
- Expand scope into later layers
- Mark INDEX `✅`
- Leave a hand-wavy plan that forces the execute model to re-design
- Plan Docker-as-default, Wails UI, or pointing Cursor at localhost
- Vendor `examples/use-kimi-on-cursor` into the product tree
