# U04 — CLI wire + JSONL migrate + docs

**Status:** Pending  
**Parent INDEX:** [INDEX.md](./INDEX.md)  
**Depends-on:** U01, U03  
**Next:** U05  
**Layer:** CLI / docs  
**Rules:** `usage.mdc`, `cobra.mdc`, `go.mdc`, `unix-packaging.mdc`, `general.mdc`

## Goal

Wire the usage dashboard into the **Cobra CLI**: optional serve-with-gateway and
standalone read-only serve. One-shot **migrate** from legacy
`{dataRoot}/usage/events.jsonl` into SQLite. Document paths, flags, and the
localhost-only invariant in README + `usage.mdc`. Note that MVP T09 CLI `usage`
should share the same SQLite store.

## Status History

| Timestamp | Event | From | To | Details | User |
| --------- | ----- | ---- | -- | ------- | ---- |
| 2026-07-19 | created | — | Pending | stub seeded for usage_ui track | |

## Requirements

- [ ] `discursive start --usage-ui` (name flexible) starts gateway **and** usage UI listener; log JSON field with dashboard URL (e.g. `usage_ui_url`)
- [ ] `discursive usage-ui` (or `usage serve`) starts **read-only** UI against existing DB when gateway is not running
- [ ] Optional flags: `--usage-ui-addr` (default `127.0.0.1:4002`)
- [ ] On store open: if `events.jsonl` exists and SQLite has no imported rows, **import** all events; then rename/archive JSONL (e.g. `events.jsonl.imported`) — idempotent
- [ ] Migrate preserves provider, model, tokens, `est_usd`, session_id, timestamps; generate IDs if missing
- [ ] `--help` documents new commands/flags
- [ ] README: how to open UI, DB path, “localhost only — not for tunnel”
- [ ] Update `.cursor/rules/usage.mdc`: SQLite path, migrate, UI listen, Chart.js embed note
- [ ] Reality note left for T09: read SQLite (not JSONL) for CLI `usage`
- [ ] Never attach usage UI to tunnel / public URL config
- [ ] No secrets in migrate logs or help text

## Implementation Plan

*(Filled by `/task-1-plan`.)*

### High-level notes (bootstrap)

- Extend `cmd/discursive` with Cobra (`AddCommand` / flags on `start`) — no Viper
- Migrate lives in `internal/usage` next to store open
- slog always JSON on stdout — URL as structured field, not a banner

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
- Full T09 doctor/usage CLI; Cursor settings; changing MVP INDEX Depends-on

### Execute model recommendation
- default (small/cheap) | large — rationale: …

## Test Plan

- Table-driven migrate: sample JSONL → SQLite row counts + totals match
- Second open skips re-import (idempotent)
- CLI help smoke (`go test` / exec) if practical
- Commands: package tests + `make verify`

## Acceptance Criteria

- [ ] Migrate test passes; archive behavior documented
- [ ] `start --usage-ui` and standalone `usage-ui` documented in `--help`
- [ ] README + `usage.mdc` updated
- [ ] T09 stub or INDEX note mentions SQLite shared store (amend T09 Reality note)
- [ ] `make verify` green; no secrets committed

## Verification

*(Filled by `/task-2-execute`)*

## Files Modified

*(Filled by `/task-2-execute`)*

## Manual test (for humans)

*(Filled by `/task-3-complete`)*

## Learnings

*(Filled by `/task-3-complete` / dialectic)*

## Reality notes

### From U01 / U03

- Write path is SQLite; UI package exposes Start/Shutdown on loopback.

### From MVP T09 (Pending)

- T09 currently says “read T03 JSONL”. When U04 lands, amend T09 Reality note:
  prefer SQLite + shared query helpers; JSONL only via migrate.

### CLI

- slog JSON only — operators: `go run ./cmd/discursive start --usage-ui | jq .`
