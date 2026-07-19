# U02 — Query / aggregate API (read path)

**Status:** Pending  
**Parent INDEX:** [INDEX.md](./INDEX.md)  
**Depends-on:** U01  
**Next:** U03  
**Layer:** Pure query  
**Rules:** `usage.mdc`, `go.mdc`, `general.mdc`

## Goal

Provide an in-process **read/aggregate** API over the SQLite usage DB so the
embedded UI (and later HTTP handlers) can show month-to-date spend, per-day /
per-session / per-model / per-provider breakdowns without scanning the whole
event history in Go. Emit **JSON-friendly DTOs** (exported fields + tags).

## Status History

| Timestamp | Event | From | To | Details | User |
| --------- | ----- | ---- | -- | ------- | ---- |
| 2026-07-19 | created | — | Pending | stub seeded for usage_ui track | |

## Requirements

- [ ] Query helpers in `internal/usage` (or `internal/usage/query`) using SQL aggregates + indexes from U01
- [ ] **Summary (MTD):** total `est_usd`, request count, prompt/completion/cache token totals for current calendar month (timezone: document — prefer local or UTC consistently)
- [ ] **By day:** for a date range (default: current month), list `{date, est_usd, request_count, …}`
- [ ] **By session:** list sessions with totals + optional last activity; support filter by range
- [ ] **By model:** totals keyed by real model id
- [ ] **By provider:** totals keyed by `moonshot` / `deepseek`
- [ ] Optional date-range filter shared across breakdowns
- [ ] DTOs with `json` tags suitable for `encoding/json` (unlike bare `SessionSummary` today)
- [ ] No HTTP server in this task (U03); pure Go API + tests
- [ ] Never return secrets; no pricing re-implementation (use stored `est_usd`)

## Implementation Plan

*(Filled by `/task-1-plan`.)*

### High-level notes (bootstrap)

- Prefer SQL `GROUP BY` over loading all rows into memory
- Keep `EstimateUSD` out of the hot read path — costs already on each event
- Design DTOs so U03 can `json.Marshal` directly into `/api/*` responses
- Session id semantics remain: one gateway process run = one `sess_…` (until CLI overrides later)

## Execution plan (filled by /task-1-plan)

**Date:**  
**Codebase snapshot:**  
**Execute model:** small/default | large (only if justified)

### Context for executor
- …

### Steps
1. … → verify: …

### Tests to add
- …

### Verify commands
- …

### Risks / pitfalls
- …

### Out of scope
- HTML/Chart.js, CLI flags, JSONL migrate, public gateway routes

### Execute model recommendation
- default (small/cheap) | large — rationale: …

## Test Plan

- Fixture DB with events spanning two months, two providers, multiple models/sessions
- Table-driven: MTD, by-day, by-session, by-model, by-provider match hand-computed totals
- Empty DB returns zeroed summary (not error)
- Commands: `go test ./internal/usage/...` then `make verify`

## Acceptance Criteria

- [ ] All breakdown helpers covered by table-driven tests
- [ ] Queries use indexes / aggregates (no full-table Go scan of all events for MTD)
- [ ] JSON tags present on public DTOs
- [ ] Tests added/updated; `make verify` green
- [ ] No secrets in DTOs or logs

## Verification

*(Filled by `/task-2-execute`)*

## Files Modified

*(Filled by `/task-2-execute`)*

## Manual test (for humans)

*(Filled by `/task-3-complete`)*

## Learnings

*(Filled by `/task-3-complete` / dialectic)*

## Reality notes

### From U01

- SQLite at `{dataRoot}/usage/usage.db`; event IDs non-empty; write path via `Store.Record`.

### From MVP T03

- Old `SessionSummary` / `LoadEvents` may still exist — migrate callers to new query API or implement them on SQLite.

### Product

- Totals shown in UI must use **provider** `est_usd` already stored — never Cursor comparison rates.
