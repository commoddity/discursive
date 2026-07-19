# U03 — Embedded localhost UI (HTML + Chart.js)

**Status:** Pending  
**Parent INDEX:** [INDEX.md](./INDEX.md)  
**Depends-on:** U02  
**Next:** U04  
**Layer:** Operator UX  
**Rules:** `usage.mdc`, `go.mdc`, `gateway.mdc`, `unix-packaging.mdc`, `general.mdc`

## Goal

Ship a **simple localhost dashboard** served by Go (`embed.FS`): plain HTML/CSS/
vanilla JS — **no SPA framework**. Top **summary panel** (month-to-date spend,
request count, tokens). **Chart.js** bar charts for spend by day, by model, and
by provider. Session list with drill-down. JSON under `/api/...` on a **separate
loopback listener** — never on the public tunnel / gateway mux.

## Status History

| Timestamp | Event | From | To | Details | User |
| --------- | ----- | ---- | -- | ------- | ---- |
| 2026-07-19 | created | — | Pending | stub seeded for usage_ui track | |

## Requirements

- [ ] Package e.g. `internal/usageui`: HTTP server + embedded static assets
- [ ] Bind **loopback only** (default `127.0.0.1:4002`; configurable)
- [ ] Static: `index.html`, CSS, JS; **Chart.js vendored** under embed (offline-first; CDN only for prototyping if needed, remove before ✅)
- [ ] **Summary panel** at top: MTD `est_usd`, request count, token totals (clear visual hierarchy)
- [ ] Bar charts: by day (current month), by model, by provider
- [ ] Session list + simple detail (totals for one `session_id`)
- [ ] JSON API (same listener), e.g.:
  - `GET /api/summary` — MTD
  - `GET /api/by-day`
  - `GET /api/by-model`
  - `GET /api/by-provider`
  - `GET /api/sessions` (+ optional `?session_id=`)
- [ ] API handlers call U02 query helpers; no duplicate pricing math
- [ ] **Must not** register these routes on the public gateway (`:4001` / tunnel)
- [ ] No Moonshot/DeepSeek/gateway secrets in HTML or JSON
- [ ] No npm, React, Vue, Astro, Wails, Tauri

## Implementation Plan

*(Filled by `/task-1-plan`.)*

### High-level notes (bootstrap)

- Chart.js: https://www.chartjs.org/ — bar chart type; keep UI intentionally sparse
- One composition: summary first, then charts, then sessions — avoid dashboard clutter
- Server constructor takes store/query handle + listen addr; `Start`/`Shutdown` for U04 wiring
- Docs for Chart.js / license: note MIT; vendor file with version comment

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
- CLI `start --usage-ui` / migrate (U04); tunnel exposure; auth beyond loopback

### Execute model recommendation
- default (small/cheap) | large — rationale: …

## Test Plan

- `net/http/httptest` against usageui server with temp DB + fixture events
- Assert `/api/summary` JSON shape and totals; static `/` returns 200 HTML
- Confirm listen address is loopback in unit test (or documented bind helper)
- Commands: `go test ./internal/usageui/...` (or package path chosen) + `make verify`

## Acceptance Criteria

- [ ] Dashboard loads against fixture DB; summary + three bar charts render with data
- [ ] Chart.js is vendored in `embed` for offline use
- [ ] No routes added to public gateway mux
- [ ] Handler tests green; `make verify` green
- [ ] No secrets in responses; no SPA framework / npm

## Verification

*(Filled by `/task-2-execute`)*

## Files Modified

*(Filled by `/task-2-execute`)*

## Manual test (for humans)

*(Filled by `/task-3-complete`)*

## Learnings

*(Filled by `/task-3-complete` / dialectic)*

## Reality notes

### From U02

- Use query DTOs / helpers — do not re-aggregate in JS from raw events.

### From gateway / tunnel

- Public Base URL is for Cursor Agent only. Usage UI must stay on `127.0.0.1`.
  Publishing usage via cloudflared is a **hard no**.

### UI stack (locked)

- No SPA; Chart.js for bars only.
