# U00 — Preliminary: JSONL → SQLite migration + daily aggregation + pretty-print

**Status:** Planned  
**Parent INDEX:** [INDEX.md](./INDEX.md)  
**Depends-on:** MVP T01–T10 (all ✅)  
**Next:** U01  
**Layer:** Persistence + CLI output  
**Rules:** `usage.mdc`, `go.mdc`, `cobra.mdc`, `general.mdc`

## Goal

Replace the JSONL store with SQLite (`modernc.org/sqlite`), add daily
aggregation to CLI `discursive usage`, and convert all CLI JSON output to
pretty-printed multiline by default. This is the foundation for U01–U05.

## Status History

| Timestamp | Event | From | To | Details | User |
| --------- | ----- | ---- | -- | ------- | ---- |
| 2026-07-20 | created | — | Pending | stub seeded from updated plan file | |
| 2026-07-20 | task-1-plan | Pending | Planned | execution plan written; 10 steps, 7 new tests, verify gates | |

## Requirements

### U00.1 — SQLite driver (already decided)

- `modernc.org/sqlite` (pure-Go, no CGo). Already in `go.sum` as indirect dep v1.54.0.
- WAL journal mode + `synchronous=NORMAL` + `busy_timeout=5000`.

### U00.2 — SQLite schema + store rewrite

- [ ] DB path: `{dataRoot}/usage/usage.db` (create `usage/` dir as today)
- [ ] Schema `events` table:
  - `id` TEXT PRIMARY KEY
  - `session_id` TEXT NOT NULL
  - `timestamp` TEXT NOT NULL (RFC 3339 UTC)
  - `provider` TEXT NOT NULL
  - `model` TEXT NOT NULL
  - `prompt_tokens` INTEGER NOT NULL DEFAULT 0
  - `completion_tokens` INTEGER NOT NULL DEFAULT 0
  - `cache_hit_tokens` INTEGER NOT NULL DEFAULT 0
  - `cache_miss_tokens` INTEGER NOT NULL DEFAULT 0
  - `est_usd` REAL NOT NULL DEFAULT 0
  - `request_id` TEXT NOT NULL DEFAULT ''
  - `latency_ms` INTEGER NOT NULL DEFAULT 0
- [ ] Indexes: `(timestamp)`, `(session_id)`, `(provider, model)`
- [ ] `Store.Record` / `RecordAndObserve` write SQLite (compute `EstUSD` as today)
- [ ] Auto-generate `Event.ID` when empty
- [ ] `LoadEvents` reads from SQLite (used by aggregator's `SessionSummary` and CLI `usage`)
- [ ] Remove JSONL file I/O (`os.O_APPEND` writes, `bufio.Scanner` parsing)
- [ ] **No migration from JSONL.** Old `events.jsonl` is ignored. Fresh start with SQLite.
- [ ] Gateway `recordUsage` path continues to call the same store API — no pricing changes

### U00.3 — Daily aggregation in CLI `discursive usage`

- [ ] Replace 3 separate JSON log lines with a **single JSON object** containing both daily totals and latest session breakdown
- [ ] Default behavior (no flags): show today's daily totals + breakdowns
- [ ] Output shape: `{date, request_count, tokens_in, tokens_out, cache_hit_tokens, cache_miss_tokens, est_usd, by_model[], cursor_reference{}, session_id}`
- [ ] Flags:
  - `--date 2026-07-19` — specific day's totals
  - `--session <id>` — specific session's breakdown (same shape, session_id instead of date)
  - `--days 7` — last N days as array of daily summaries
- [ ] Implementation: SQL `GROUP BY date(timestamp)` queries — no full-table Go scans

### U00.4 — Pretty-printed JSON by default

- [ ] Add shared `emitPretty(v any)` helper in `internal/cli/` using `json.NewEncoder` with `SetIndent("", "  ")`
- [ ] Commands to update:
  - `discursive status` — currently 3 `slog.Info` calls → single pretty object
  - `discursive usage` — already addressed in U00.3 (single object)
  - `discursive doctor` — combine per-check lines into single `{"checks": [...]}` object
  - `discursive start` — keep JSON log lines for piping; `*_summary` events become single objects
  - `discursive` (root) — already single `slog.Info("discursive ready", ...)` → fine
  - `discursive init` — finish card structured output → fine as-is
- [ ] Interactive prompts (init, set key prompts) continue to stderr — only structured stdout gets pretty-printed

## Implementation Plan

*(Filled by `/task-1-plan` — do not invent during bootstrap beyond high-level notes.)*

### High-level notes (bootstrap)

- Current store: `internal/usage/store.go` — JSONL at `{dataRoot}/usage/events.jsonl`
- Current CLI usage: 3 separate `slog.Info` lines (`usage_session`, `usage_by_model`, `usage_cursor_reference`)
- Current CLI output: all compact JSON via `slog.NewJSONHandler(os.Stdout, ...)`
- Only `discursive logs` pretty-prints today
- `modernc.org/sqlite` already in `go.sum` — needs to become a direct dependency
- `setupLogger()` in `root.go` creates the slog JSON handler — pretty-print changes how commands emit, not the slog handler itself

## Execution plan (filled by /task-1-plan)

**Date:** 2026-07-20
**Codebase snapshot:** MVP T01-T10 complete; JSONL store at `{dataRoot}/usage/events.jsonl`;
`modernc.org/sqlite` v1.54.0 in `go.sum` as indirect; all CLI output is compact JSON via slog
**Execute model:** small/default

### Context for executor

U00 replaces the JSONL usage store (`internal/usage/store.go`) with SQLite via `modernc.org/sqlite`,
rewrites CLI `discursive usage` to emit a single daily-aggregated JSON object instead of 3 separate
slog lines, and makes all CLI commands pretty-print JSON by default (no more `| jq` needed).

**Key files:**
- `internal/usage/store.go` — JSONL store to replace with SQLite
- `internal/usage/store_test.go` — existing store tests (Record + SessionSummary via JSONL)
- `internal/usage/pricing.go` — `EstimateUSD`, `RoundUSD`, `CursorComparisonReference` (do NOT change)
- `internal/usage/aggregator.go` — `LogEvent`, `LogSummary`, `NewAggregator` (do NOT change)
- `internal/cli/usage.go` — current CLI usage (3 slog lines); rewrite to single pretty JSON object
- `internal/cli/root.go` — `setupLogger()`, `setupLoggerWithLevel()`, `NewRoot()` (add `emitPretty` helper)
- `internal/cli/status.go` — 3 slog calls to consolidate into single pretty object
- `internal/cli/doctor.go` — per-check slog lines to consolidate into `{"checks": [...]}`
- `internal/cli/start.go` — `serveGateway` logs `gateway_starting` (add `usage_ui_url` field; UI listener deferred to U03/U04)
- `internal/gateway/server.go` — `NewServer` creates `usage.NewStore(cfg.DataRoot)` + `NewAggregator(0)` (unchanged; SQLite transparent)
- `internal/gateway/stream.go` — `recordUsage` calls `s.store.RecordAndObserve(s.agg, ev)` (unchanged; SQLite transparent)
- `internal/doctor/doctor.go` — `Check` and `Report` structs with `json` tags (use for consolidation)
- `go.mod` — `modernc.org/sqlite` is indirect; becomes direct dependency after first import

**Invariants:**
- Pricing (`EstimateUSD`, `RoundUSD`, `CursorComparisonReference`) unchanged
- Aggregator (`NewAggregator`, `Observe`, `Flush`, `LogEvent`, `LogSummary`) unchanged
- Gateway `recordUsage` call sites (3 in proxy.go, 1 in stream.go) unchanged — call same Store API
- No Cursor comparison rows in billing math
- No secrets in DB rows or logs
- Table-driven Go tests required for new/changed behavior (per `go.mdc`)
- No `//nolint` comments (fix root cause instead)
- Interactive prompts continue to stderr; only structured stdout gets pretty-printed
- WAL journal mode + `synchronous=NORMAL` + `busy_timeout=5000`

### Steps

**Step 1: Promote `modernc.org/sqlite` to direct dependency**
- Run `go get modernc.org/sqlite@v1.54.0` (already the version in go.sum)
- Verify: `go mod tidy` + `go build ./...` compiles (no import yet, just ensures the module is direct)

**Step 2: Rewrite `internal/usage/store.go` — SQLite-backed store**
- Replace `Store` struct: remove `eventsPath string`, add `db *sql.DB`
- `NewStore` opens/creates the DB at `{dataRoot}/usage/usage.db` with pragmas:
  ```go
  db.Exec("PRAGMA journal_mode=WAL")
  db.Exec("PRAGMA synchronous=NORMAL")
  db.Exec("PRAGMA busy_timeout=5000")
  ```
- Create schema table `events` with columns matching `Event` struct fields (timestamp as RFC 3339 TEXT)
- Create indexes: `idx_events_timestamp`, `idx_events_session`, `idx_events_provider_model`
- `Record` method: INSERT into SQLite instead of JSONL append. Auto-generate `Event.ID` when empty (use `crypto/rand` hex + `evt_` prefix). Compute `EstUSD` via `EstimateUSD` as before
- `LoadEvents` method: SELECT from SQLite ordered by timestamp. Return `[]Event` as today
- `RecordAndObserve` — unchanged signature; calls `Record` then feeds aggregator
- `SessionSummary` — use `LoadEvents` (still needed for aggregator compatibility; U02 will optimize)
- Remove all JSONL I/O: `bufio`, `os.O_APPEND`, `events.jsonl` path
- Keep `Event`, `SessionSummary`, `ModelTotals` structs unchanged
- Remove `import "bufio"` and unused imports
- Verify: `go build ./internal/usage/` compiles

**Step 3: Update `internal/usage/store_test.go` for SQLite**
- Replace JSONL assertions with SQLite-specific checks:
  - Record → reopen DB via `NewStore` → `LoadEvents` returns same events
  - Verify `Event.ID` is non-empty after `Record` (auto-generated)
  - Verify provider, model, token counts, est_usd match
  - Test empty DB: `LoadEvents` returns empty slice (not error)
  - Test schema exists: query `sqlite_master` for `events` table + indexes
  - Existing `TestStoreRecordAndSessionSummary` adapts cleanly — only store backend changes
- Keep table-driven pattern (`[]struct` + `t.Run`)
- Verify: `go test ./internal/usage/` passes

**Step 4: Verify gateway + pricing tests still pass**
- `go test ./internal/usage/` — all tests pass (pricing, aggregator, env)
- `go test ./internal/gateway/` — gateway tests pass (store is created internally via `NewServer`)
- Pricing tests (`TestEstimateUSD`, `TestEstimateUSDNeverUsesCursorComparison`) must pass unchanged
- Verify: `go test ./internal/usage/ ./internal/gateway/` green

**Step 5: Create `internal/cli/output.go` — `emitPretty` helper**
- New file `internal/cli/output.go`:
  ```go
  package cli
  
  import (
      "encoding/json"
      "os"
  )
  
  // emitPretty writes a single pretty-printed JSON object to stdout.
  func emitPretty(v any) error {
      enc := json.NewEncoder(os.Stdout)
      enc.SetIndent("", "  ")
      return enc.Encode(v)
  }
  ```
- Verify: `go build ./internal/cli/` compiles

**Step 6: Consolidate `discursive doctor` into single `{"checks": [...]}` object**
- In `internal/cli/doctor.go`, replace the `for _, check := range report.Checks` loop + slog calls with:
  ```go
  if err := emitPretty(report); err != nil {
      return err
  }
  ```
- `doctor.Report` already has `json:"ok"` and `json:"checks"` tags — it marshals cleanly
- Remove import `log/slog` if no other slog calls remain in the file
- Keep `countFailed` and error return for non-zero exit
- Update `Long` help text: remove `Each check is a JSON line...` language, replace with `Prints a single JSON object with all checks and their results.`
- Verify: `go build ./internal/cli/` + `go test ./internal/cli/` passes (doctor tests check exit code, not output format — existing tests cover this)

**Step 7: Consolidate `discursive status` into single pretty object**
- In `internal/cli/status.go`, replace the 3 separate `slog.Info` calls with a single struct marshaled via `emitPretty`:
  ```go
  output := struct {
      Version       string   `json:"version"`
      AliasModel    string   `json:"alias_model"`
      RealModel     string   `json:"real_model"`
      Provider      string   `json:"provider"`
      HasMoonshotKey bool    `json:"has_moonshot_key"`
      HasDeepSeekKey bool    `json:"has_deepseek_key"`
      TunnelMode    string   `json:"tunnel_mode"`
      PublicURL     string   `json:"public_url"`
      LocalPort     int      `json:"local_port"`
      DataRoot      string   `json:"data_root"`
      GatewayKey    *string  `json:"gateway_key,omitempty"`
      GatewayKeyMasked *string `json:"gateway_key_masked,omitempty"`
      Models        []string `json:"models"`
      Running       bool     `json:"running"`
      PID           int      `json:"pid"`
      UptimeSeconds int64    `json:"uptime_seconds"`
      LogFile       string   `json:"log_file"`
      LogSize       string   `json:"log_size"`
  }{...}
  ```
- Populate `GatewayKey` (when `--show-key`) or `GatewayKeyMasked` (default) — never both
- Remove `slog.Info` calls; remove `log/slog` import if unused
- Update `Long` help text: remove `Each check is a JSON line...` reference
- Verify: `go build ./internal/cli/` + `go test ./internal/cli/` passes
- **Important:** existing status tests capture stdout via pipe and check for field names like `"alias_model"`, `"models"`, `"gateway_key"`, `"gateway_key_masked"`. The tests check `strings.Contains` on full output — since the output is now a single pretty-printed JSON object, all these substrings will still be found. No test changes needed.

**Step 8: Rewrite `discursive usage` — single daily-aggregated JSON object with flags**
- In `internal/cli/usage.go`, rewrite `RunE`:
  - Accept flags: `--date` (string, e.g. `2026-07-19`), `--session` (string), `--days` (int, default not set)
  - Open SQLite store via `usage.NewStore(dataRoot)`
  - Run SQL GROUP BY queries (not full-table Go scans):
    - Default (no flags): `SELECT ... GROUP BY date(timestamp) WHERE date(timestamp) = date('now')` → today's totals + latest session
    - `--date X`: `GROUP BY date(timestamp) WHERE date(timestamp) = ?`
    - `--session X`: `GROUP BY session_id WHERE session_id = ?`
    - `--days N`: `GROUP BY date(timestamp) ORDER BY date(timestamp) DESC LIMIT ?` → array of daily summaries
  - Output single JSON object matching plan shape:
    ```json
    {
      "date": "2026-07-20",
      "request_count": 42,
      "tokens_in": 615135,
      "tokens_out": 719,
      "cache_hit_tokens": 461312,
      "cache_miss_tokens": 153823,
      "est_usd": 0.069,
      "by_model": [{"model": "...", "provider": "...", "request_count": 4, "tokens_in": 615135, "tokens_out": 719, "est_usd": 0.069}],
      "cursor_reference": {"input_per_1m": 0.5, "cache_per_1m": 0.2, "output_per_1m": 2.5},
      "session_id": "sess_..."
    }
    ```
  - `--days N` returns a JSON array of daily summary objects (same shape minus session_id)
  - Use `emitPretty` for output
  - When no events found: emit `{"date": "...", "request_count": 0, "tokens_in": 0, ...}` (zeroed, not error)
  - Remove imports `log/slog` if no slog calls remain
  - Update `Long` help text: document new flags, single-object output, daily aggregation
- Define SQL query helpers as unexported functions in `store.go` (or a new `store_query.go`):
  - `queryDailyTotals(db, date string) (DailySummary, error)`
  - `querySessionDetail(db, sessionID string) (SessionDetail, error)`
  - `queryLastNDays(db, n int) ([]DailySummary, error)`
  - These use `database/sql` with `QueryRow`/`Query` and `Scan` into DTO structs with `json` tags
- New DTO types in `internal/usage/` (in `store.go` or new `dto.go`):
  ```go
  type DailySummary struct {
      Date           string        `json:"date"`
      RequestCount   uint64        `json:"request_count"`
      TokensIn       uint64        `json:"tokens_in"`
      TokensOut      uint64        `json:"tokens_out"`
      CacheHitTokens uint64        `json:"cache_hit_tokens"`
      CacheMissTokens uint64       `json:"cache_miss_tokens"`
      EstUSD         float64       `json:"est_usd"`
      ByModel        []ModelBreakdown `json:"by_model"`
      SessionID      string        `json:"session_id,omitempty"`
  }
  
  type ModelBreakdown struct {
      Model        string  `json:"model"`
      Provider     string  `json:"provider"`
      RequestCount uint64  `json:"request_count"`
      TokensIn     uint64  `json:"tokens_in"`
      TokensOut    uint64  `json:"tokens_out"`
      EstUSD       float64 `json:"est_usd"`
  }
  ```
- Verify: `go build ./internal/usage/ ./internal/cli/` compiles

**Step 9: Update `runRoot` (bare `discursive` command) to use `emitPretty`**
- In `internal/cli/root.go`, `runRoot` currently calls `slog.Info("discursive ready", attrs...)`
- Replace with a struct + `emitPretty`:
  ```go
  output := map[string]any{
      "data_root": root,
      "local_port": settings.LocalPort,
      "real_model": settings.RealModel,
      "alias_model": settings.AliasModel,
      "has_moonshot_key": settings.HasMoonshotKey(),
      "has_deepseek_key": settings.HasDeepSeekKey(),
      "version": Version,
  }
  // Add gateway key attrs
  ...
  emitPretty(output)
  ```
- `gatewayKeyLogAttrs` returns `[]any` — convert to map entries
- Verify: `go build ./internal/cli/`

**Step 10: Final test and verify**
- Run `go test ./internal/usage/` — all tests pass (store, pricing, aggregator, env)
- Run `go test ./internal/cli/` — all tests pass (doctor, status, usage, root, etc.)
- Run `go test ./internal/gateway/` — gateway tests pass
- Run `go build ./...` — compiles
- Run `make verify` — lint + test + build all green
- Verify: `make verify` exit code 0

### Tests to add

**In `internal/usage/store_test.go`:**
1. `TestStore_SQLite_RecordAndLoad` — Record 3 events → LoadEvents returns 3 with non-empty IDs
2. `TestStore_SQLite_AutoID` — Record event with empty ID → stored event has non-empty ID
3. `TestStore_SQLite_EmptyLoad` — New store, no records → LoadEvents returns empty slice
4. `TestStore_SQLite_SchemaExists` — query `sqlite_master` for `events` table + 3 indexes
5. `TestStore_SQLite_DailyQuery` — insert events on 2 different days → `queryDailyTotals` returns correct per-day aggregates
6. `TestStore_SQLite_SessionQuery` — insert events for 2 sessions → `querySessionDetail` returns correct breakdown
7. `TestStore_SQLite_LastNDays` — insert events across 5 days → `queryLastNDays(3)` returns 3 most recent

**In `internal/cli/usage_test.go`:**
8. Update `TestUsageCmd_EmptyStore` — should print zeroed JSON, not `usage_empty` slog line
9. `TestUsageCmd_TodayEmpty` — empty DB, no events today → zeroed daily JSON object
10. Run existing `TestUsageCmd_Help` unchanged

**In `internal/cli/status_test.go`:**
11. Existing tests check `strings.Contains(output, field)` — they pass since pretty-printed JSON still contains field names as strings. No changes needed.

### Verify commands

```bash
# After each step:
go build ./internal/usage/
go build ./internal/cli/

# After step 6 (doctor consolidation):
go test ./internal/cli/ -run TestDoctor

# After step 7 (status consolidation):
go test ./internal/cli/ -run TestStatus

# After step 8 (usage rewrite):
go test ./internal/usage/ -run TestStore
go test ./internal/cli/ -run TestUsage

# Final gate:
go test ./internal/usage/ ./internal/cli/ ./internal/gateway/ ./internal/doctor/
make verify
```

### Risks / pitfalls

1. **slog output captured in tests.** Status tests capture stdout via `os.Pipe` and parse lines as JSON. The pretty-print output is a single multiline JSON object (no longer individual slog lines). The tests use `strings.Contains(output, "\"alias_model\"")` — these assertions still pass because field names are JSON strings. However, if a test splits on `\n` and parses each line individually (like `TestStatusCmd_ModelsAreJSON`), it must be updated: `strings.Split` on multiline pretty JSON gives individual lines that are NOT valid JSON on their own. **Fix:** In `TestStatusCmd_ModelsAreJSON`, replace the line-by-line parsing with a single `json.Unmarshal` of the full output.

2. **Doctor tests check exit code, not output format.** `TestDoctorCmd_NoKeys` and `TestDoctorCmd_RunsWithoutCrash` just check `err != nil` / `err == nil`. They will pass regardless of output format change. No test changes needed.

3. **Usage tests expect `usage_empty` slog message.** `TestUsageCmd_EmptyStore` currently expects success (no error). After the rewrite, it should produce a zeroed JSON object instead of slog `usage_empty`. The test will still pass since no error is returned. But add an assertion that stdout contains `"request_count": 0` for the new behavior.

4. **`modernc.org/sqlite` already in go.sum.** Just needs `go get` to promote from indirect to direct. No version conflict risk.

5. **Aggregator still calls `LoadEvents` / `SessionSummary`.** The aggregator's `flushLocked` calls `summarizeEvents` which sums in-memory `a.events` — it does NOT call `LoadEvents`. The only aggregator path that touches store is `RecordAndObserve` → `Record` → SQLite. `SessionSummary` in `store.go` is only called by old CLI usage (which gets rewritten). Verification: `go test ./internal/usage/` confirms aggregator tests pass.

6. **`go.mod` replace directives.** If `modernc.org/sqlite` upgrade causes transitive dep issues, pin to v1.54.0 (already in go.sum).

### Out of scope

- `embed.FS`, Chart.js, HTML/CSS/JS static assets (U03)
- HTTP API routes (U03)
- CLI `start --usage-ui` integration (U04)
- README / `usage.mdc` updates (U04)
- Tunnel configuration changes
- JSONL migration from old `events.jsonl` (fresh SQLite start — no import)
- U02 query API abstraction (U00 does raw SQL GROUP BY; U02 wraps in clean API)

### Execute model recommendation

- default (small/cheap) — the work is well-bounded: swap JSONL for SQLite (known schema, known driver), add 3 SQL GROUP BY queries, wrap CLI output with `emitPretty`. No architectural decisions remain. The test suite is table-driven and the existing tests anchor correctness.

## Test Plan

- Table-driven: SQLite Record → reopen DB → row fields match (incl. non-empty `id`)
- Daily aggregation: fixture events spanning multiple days → `GROUP BY date(timestamp)` totals match hand-computed
- Empty DB: CLI `usage` returns zeroed summary (not error)
- Pretty-print: all commands emit valid multiline JSON on stdout
- Existing pricing / aggregator tests still pass
- Commands: `go test ./internal/usage/... ./internal/cli/...` then `make verify`

## Acceptance Criteria

- [ ] SQLite file created under temp `dataRoot` on first `Record`
- [ ] Event IDs always non-empty after `Record`
- [ ] JSONL file I/O removed from `store.go`
- [ ] CLI `usage` emits single JSON object with daily + session data
- [ ] `--date`, `--session`, `--days` flags work
- [ ] All CLI commands pretty-print JSON by default
- [ ] Gateway compile/tests that touch usage still green
- [ ] `EstimateUSD` / pricing tables unchanged
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
  events into memory. U00 replaces JSONL with SQLite as the primary store.
- `Event.ID` exists but is often empty — U00 must fill it.
- Idle aggregator default / `DISCURSIVE_USAGE_IDLE` unchanged.

### From MVP T05

- Gateway creates `usage.NewStore(cfg.DataRoot)` and `sessionID` per process; keep that wiring.
- `recordUsage` in `proxy.go` (3 call sites) flows through `stream.go:recordUsage` → `store.RecordAndObserve(agg, ev)`.

### From current CLI

- `discursive usage` reads JSONL, calls `LoadEvents()` + `SessionSummary()`, emits 3 separate `slog.Info` lines.
- All commands use `slog.NewJSONHandler(os.Stdout, opts)` — compact JSON.
- `discursive logs` is the only command with pretty-printed output (colored prefixes + `json.MarshalIndent`).
- `discursive doctor` emits one `slog.Info`/`slog.Warn` per check.
- `discursive status` emits 3 `slog.Info` calls (status, status_models, status_runtime).

### Product

- Optional track — MVP T01–T10 all complete. No JSONL migration (fresh SQLite start).
