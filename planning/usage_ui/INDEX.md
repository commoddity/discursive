# Optional track — Usage / pricing UI

**Product**: Optional localhost usage dashboard for Custom Cursor Gateway.  
**Scope**: Post-MVP / optional — does **not** block
[`planning/phases/`](../phases/INDEX.md) T08–T10.  
**MVP dependency**: Requires T03 usage store (✅). Prefer landing U01 before or
with T09 so CLI `usage` and this UI share one SQLite store.

**Workflow**: `/task-1-plan U0X` → `/task-2-execute U0X` → `/task-3-complete U0X`
(complete **pushes** by default; `--no-push` to skip; always emits **Manual test**)

**Rules hub**: [`CLAUDE.md`](../../CLAUDE.md) → `.cursor/rules/general.mdc`  
**Domain**: `.cursor/rules/usage.mdc` (+ `go.mdc`, `unix-packaging.mdc`,
`gateway.mdc` when wiring)

## Status legend

| Status       | Meaning                                                              |
| ------------ | -------------------------------------------------------------------- |
| `Pending`    | Not planned yet                                                      |
| `Planned`    | `/task-1-plan` filled Execution plan                                 |
| `InProgress` | `/task-2-execute` running                                            |
| `✅`          | Closed via `/task-3-complete` (INDEX only — never write `Done` here) |

## Task table

<<<<<<< Updated upstream
| ID  | Title                                      | Status  | Depends-on   | Next | Layer        | Task file                                                      |
| --- | ------------------------------------------ | ------- | ------------ | ---- | ------------ | -------------------------------------------------------------- |
| U01 | SQLite usage store (write path)            | Pending | MVP T03 (✅)  | U02  | Persistence  | [U01-sqlite-usage-store.md](./U01-sqlite-usage-store.md)       |
| U02 | Query / aggregate API (read path)          | Pending | U01          | U03  | Pure query   | [U02-usage-query-api.md](./U02-usage-query-api.md)             |
| U03 | Embedded localhost UI (HTML + Chart.js)    | Pending | U02          | U04  | Operator UX  | [U03-embedded-usage-ui.md](./U03-embedded-usage-ui.md)         |
| U04 | CLI wire + JSONL migrate + docs            | Pending | U01, U03     | U05  | CLI / docs   | [U04-cli-wire-migrate.md](./U04-cli-wire-migrate.md)           |
| U05 | Usage UI smoke / verify                    | Pending | U04          | —    | E2E (track)  | [U05-usage-ui-smoke.md](./U05-usage-ui-smoke.md)               |
=======
| ID  | Title                                                | Status  | Depends-on          | Next | Layer              | Task file                                                              |
| --- | ---------------------------------------------------- | ------- | ------------------- | ---- | ------------------ | ---------------------------------------------------------------------- |
| U00 | SQLite migration + daily agg + pretty-print          | Planned | MVP T01–T10 (✅)     | U01  | Persistence + CLI  | [U00-sqlite-migration-daily-agg.md](./U00-sqlite-migration-daily-agg.md) |
| U01 | SQLite usage store (write path polish)               | Pending | U00                 | U02  | Persistence        | [U01-sqlite-usage-store.md](./U01-sqlite-usage-store.md)               |
| U02 | Query / aggregate API (read path)                    | Pending | U01                 | U03  | Pure query         | [U02-usage-query-api.md](./U02-usage-query-api.md)                     |
| U03 | Embedded localhost UI (HTML + Chart.js)              | Pending | U02                 | U04  | Operator UX        | [U03-embedded-usage-ui.md](./U03-embedded-usage-ui.md)                 |
| U04 | CLI wire + docs (always-on UI listener)              | Pending | U00, U03, T05 (✅)  | U05  | CLI / docs         | [U04-cli-wire.md](./U04-cli-wire.md)                                   |
| U05 | Usage UI smoke / verify                              | Pending | U04                 | —    | E2E (track)        | [U05-usage-ui-smoke.md](./U05-usage-ui-smoke.md)                       |
>>>>>>> Stashed changes

## Layer map

```text
MVP T03 pricing/usage (JSONL) ✅
 └─ U01 SQLite write path (+ event IDs)
      └─ U02 query / aggregates (MTD, day, session, model, provider)
           └─ U03 embed HTML + Chart.js on localhost
                └─ U04 start --usage-ui / usage-ui + JSONL migrate + docs
                     └─ U05 smoke (curl API + browser check)
```

## Architecture (locked)

```text
  Cursor → tunnel → gateway (:4001, public HTTPS)
                         │
                         ├─ Record Event → SQLite {dataRoot}/usage/usage.db
                         │
                         └─ optional --usage-ui → localhost :4002 (HTML + /api)
                                                    ↑
                         discursive usage-ui ──────┘  (read-only when gateway stopped)
```

| Decision     | Choice |
| ------------ | ------ |
| Persistence  | SQLite via pure-Go `modernc.org/sqlite`; written by `discursive` on each completion |
| Path         | `{dataRoot}/usage/usage.db` |
| JSONL        | One-time import if `events.jsonl` exists; then SQLite is source of truth |
| UI stack     | **No SPA framework.** Go `embed.FS` + HTML/CSS/vanilla JS |
| Charts       | **[Chart.js](https://www.chartjs.org/)** — prefer vendored under `embed` (offline) |
| Serving      | Loopback only (default `127.0.0.1:4002`); **never** via Cursor tunnel |
| Auth         | Bind loopback; no Moonshot/DeepSeek keys in UI or JSON |
| Features     | Top summary (MTD spend, requests, tokens); bars by day / model / provider; session list |

## Hard constraints (all U-tasks)

- Host Go CLI/daemon; **not** Docker-primary; **no** Wails / Vue / React / Astro / Tauri / npm for this UI
- Do **not** change MVP `planning/phases/` Depends-on or claim this track is required for T10
- Pricing math stays in `internal/usage` (`EstimateUSD`); never price Cursor comparison rows
- Usage UI listener is **separate** from the public gateway mux — no `/usage` on the tunnel
- Never log or return secrets
- **Go tests:** table-driven for new store/query/handler behavior; `make verify` is the gate
- **Branches:** stub stem only (e.g. `U01-sqlite-usage-store` — no `planning/usage_ui/` prefix)
- **Close-out:** `/task-3-complete` pushes by default (`--no-push` to skip) + Manual test
- Prefer **latest stable** Go deps when adding `modernc.org/sqlite` / Chart.js vendor pin

## Explicit non-goals

- Not a replacement for MVP T09 CLI `usage` / `doctor` (CLI remains; UI is companion)
- Not part of MVP T10 Cursor Agent CoS
- No public HTTPS usage dashboard
- No desktop shell

## How to work

1. Finish or pause MVP as needed — this track is optional and parallel  
2. `/task-1-plan U01` (prefer large model for first plan)  
3. `/task-2-execute U01` → `/task-3-complete U01`  
4. Continue U02…U05  

**Recommendation:** land **U01** before or while refining T09 so both surfaces share SQLite.
