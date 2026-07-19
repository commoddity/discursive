# U01 — SQLite usage store (write path)

**Status:** Pending  
**Parent INDEX:** [INDEX.md](./INDEX.md)  
**Depends-on:** MVP T03 (✅)  
**Next:** U02  
**Layer:** Persistence  
**Rules:** `usage.mdc`, `go.mdc`, `unix-packaging.mdc`, `general.mdc`

## Goal

Replace JSONL-as-primary persistence with an **SQLite** usage database written by
the `discursive` binary on every recorded completion. Keep pricing
(`EstimateUSD`), slog aggregator behavior, and the gateway `recordUsage` call
sites working — only the store backend changes. Generate stable non-empty event
IDs. Leave read/aggregate HTTP and UI to later U-tasks.

## Status History

| Timestamp | Event | From | To | Details | User |
| --------- | ----- | ---- | -- | ------- | ---- |
| 2026-07-19 | created | — | Pending | stub seeded for usage_ui track | |

## Requirements

- [ ] Add pure-Go SQLite (`modernc.org/sqlite`) dependency; pin latest stable unless documented otherwise
- [ ] DB path: `{dataRoot}/usage/usage.db` (create `usage/` dir as today)
- [ ] Schema `events` aligned with `usage.Event`:
  - `id` (TEXT PK), `session_id`, `timestamp`, `provider`, `model`
  - `prompt_tokens`, `completion_tokens`, `cache_hit_tokens`, `cache_miss_tokens`
  - `est_usd`, `request_id`, `latency_ms`
- [ ] Indexes: `(timestamp)`, `(session_id)`, `(provider, model)`
- [ ] `Store.Record` / `RecordAndObserve` write SQLite (compute `EstUSD` as today)
- [ ] Auto-generate `Event.ID` when empty (e.g. UUID / `evt_` + hex)
- [ ] Gateway `recordUsage` path continues to call the same store API — no pricing changes
- [ ] Aggregator / DEBUG `usage` / INFO `usage_summary` unchanged
- [ ] No secrets in DB rows or slog
- [ ] JSONL import is **out of scope** here (U04); optional: keep append JSONL as dual-write only if it reduces risk — prefer SQLite-only write once tests pass

## Implementation Plan

*(Filled by `/task-1-plan` — do not invent during bootstrap beyond high-level notes.)*

### High-level notes (bootstrap)

- Extend or replace [`internal/usage/store.go`](../../internal/usage/store.go); keep package `usage` as the single write API
- Study current `Event` / `SessionSummary` / `LoadEvents` — update callers that assume JSONL
- Cursor settings (T08) may also use SQLite later — share the *driver*, not the same DB file
- Reference: INDEX locked decisions; `.cursor/rules/usage.mdc`

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
- UI, HTTP query API, JSONL migrate, CLI flags

### Execute model recommendation
- default (small/cheap) | large — rationale: …

## Test Plan

- Table-driven: Record → reopen DB → row fields match (incl. non-empty `id`, `provider`)
- Concurrent Record smoke (single-writer expectation documented)
- Existing pricing / aggregator tests still pass
- Commands: `go test ./internal/usage/...` then `make verify`

## Acceptance Criteria

- [ ] SQLite file created under temp `dataRoot` on first `Record`
- [ ] Event IDs always non-empty after `Record`
- [ ] Gateway compile/tests that touch usage still green
- [ ] `EstimateUSD` / pricing tables unchanged; Cursor comparison still unused for billing
- [ ] Tests added/updated for new store behavior (table-driven)
- [ ] Full lint + test verify suite green (`make verify`)
- [ ] No secrets committed; no Moonshot/DeepSeek keys in DB

## Verification

*(Filled by `/task-2-execute`; re-confirmed by `/task-3-complete`)*

## Files Modified

*(Filled by `/task-2-execute`)*

## Manual test (for humans)

*(Filled by `/task-3-complete`)*

## Learnings

*(Filled by `/task-3-complete` / dialectic)*

## Reality notes

### From MVP T03

- Today: `{dataRoot}/usage/events.jsonl`, `Store.Record`, `SessionSummary` loads **all**
  events into memory. U01 makes SQLite primary; `LoadEvents` / summary helpers must
  either query SQLite or be replaced in U02.
- `Event.ID` exists but was often empty — U01 must fill it.
- Idle aggregator default / `DISCURSIVE_USAGE_IDLE` unchanged.

### From MVP T05

- Gateway creates `usage.NewStore(cfg.DataRoot)` and `sessionID` per process; keep that wiring.

### Product

- Optional track — do not block T08–T10. Prefer finishing U01 before T09 CLI reads the store.
