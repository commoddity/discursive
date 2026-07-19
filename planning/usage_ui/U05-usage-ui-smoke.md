# U05 — Usage UI smoke / verify

**Status:** Pending  
**Parent INDEX:** [INDEX.md](./INDEX.md)  
**Depends-on:** U04  
**Next:** —  
**Layer:** E2E (this track)  
**Rules:** `usage.mdc`, `go.mdc`, `general.mdc`

## Goal

Prove the optional usage UI track end-to-end: seed or record events → SQLite →
localhost `/api/summary` and related breakdowns → dashboard loads with summary
panel and Chart.js bars. Document **Manual test** commands for humans. Confirm
`make verify` is green for the whole touch surface. This is **not** MVP T10
(Cursor Agent smoke).

## Status History

| Timestamp | Event | From | To | Details | User |
| --------- | ----- | ---- | -- | ------- | ---- |
| 2026-07-19 | created | — | Pending | stub seeded for usage_ui track | |

## Requirements

- [ ] Documented smoke path (script or test + manual steps):
  1. Temp or portable data root
  2. Insert fixture events (or short live gateway call if keys present — optional)
  3. Start `usage-ui` (or `start --usage-ui`)
  4. `curl` `/api/summary`, `/api/by-day`, `/api/by-model`, `/api/by-provider`
  5. Open `http://127.0.0.1:4002/` — summary + charts visible
- [ ] Assert loopback-only (smoke notes: do not put usage URL in Cursor Base URL)
- [ ] If JSONL fixture present: migrate runs once; curl totals match expectations
- [ ] `make verify` green
- [ ] Manual test section filled with copy-paste commands + what to look for
- [ ] INDEX U01–U05 can all be `✅` after this close-out; MVP INDEX unchanged except optional-track pointer

## Implementation Plan

*(Filled by `/task-1-plan`.)*

### High-level notes (bootstrap)

- Prefer automated handler/API smoke in `go test`; browser check is Manual test
- Do not require live Moonshot/DeepSeek keys for AC (fixtures enough)
- Optional live path: record one completion then refresh UI — nice-to-have, not AC

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
- MVP T10 Agent smoke; public tunnel; SPA rewrite

### Execute model recommendation
- default (small/cheap) | large — rationale: …

## Test Plan

- Integration-style test: temp DB + usageui server + curl-equivalent client checks
- `make verify`
- Manual: browser open once on developer machine

## Acceptance Criteria

- [ ] Automated smoke (or package integration test) proves API totals for fixtures
- [ ] Manual test commands recorded and runnable on macOS/Linux
- [ ] Confirmed usage UI not exposed via tunnel config
- [ ] `make verify` green
- [ ] Track INDEX ready for full `✅` close-out of U05

## Verification

*(Filled by `/task-2-execute`)*

## Files Modified

*(Filled by `/task-2-execute`)*

## Manual test (for humans)

*(Filled by `/task-3-complete` — example shape:)*

```bash
# Example (adjust after implement):
go run ./cmd/discursive usage-ui --portable
curl -s http://127.0.0.1:4002/api/summary | jq .
# Browser: http://127.0.0.1:4002/ — MTD panel + bar charts
```

## Learnings

*(Filled by `/task-3-complete` / dialectic — update `usage.mdc` if new failure modes)*

## Reality notes

### Track complete means

- Optional operator UI works locally; MVP can still be InProgress on T08–T10.

### Non-goals reminder

- Not a substitute for T09 CLI; not Agent CoS.
