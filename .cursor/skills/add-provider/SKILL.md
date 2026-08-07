---
name: add-provider
description: >-
  Adds an entire new AI provider to the Discursive gateway end-to-end. Covers
  Go constants, config, key management, alias routing, sanitizer thinking
  policy, optimizer, proxy dispatch, CLI (init/set/status/wizard), doctor
  checks, pricing map, usage UI dashboard (colors, logos, balance fetch,
  effort labels, pricing table), rules files, README sections, and tests.
  Manual-only — /add-provider.
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /add-provider — Add a new AI provider

Adds a **net-new AI provider** with one or more models to the Discursive
gateway. This is a superset of `/add-model` because it also creates the
provider plumbing (key store, base URL, thinking policy, balance check,
dashboard colors/logo, etc.).

**What you need from the user:**

| Required | Description |
| -------- | ----------- |
| Provider name | Lowercase id, e.g. `openai` or `anthropic` |
| Provider display name | Human-readable, e.g. `OpenAI` |
| One or more models | Real model ids + cursor aliases + pricing |
| Upstream base URL | OpenAI-compat root, e.g. `https://api.openai.com/v1` |
| Docs page | Provider main docs URL |
| Pricing page URL | For README + dashboard links |
| Logo | SVG or PNG (36-40px height recommended) placed in `.github/img/` |
| Key setup page URL | Where users get their API key |
| Balance check endpoint | If available: URL path + response shape |
| Supported thinking mode | Does the provider support `reasoning_effort`? `thinking` object? Neither? |

**Optional:**

| Item | Description |
| ---- | ----------- |
| Top-up URL | Where users add funds (dashboard) |
| Cache pricing | Whether cache-hit tokens have a separate tier |

## Steps

Work in this exact order. Stop at each verify gate — do not advance on
broken test/build. The placeholder `<Name>` / `<name>` refers to the new
provider (e.g. `OpenAI` / `openai`).

### 1. Provider constant + base URLs

**File:** `internal/config/urls.go`

- [ ] Add `Provider<Name> Provider = "<name>"` to the `const` block (~line 18).
- [ ] Add `Default<Name>BaseURL = "https://..."` constant (~line 51).
- [ ] Add `case Provider<Name>:` to `UpstreamBaseURL()` (~line 54).

**File:** `internal/config/urls_test.go`

- [ ] Add test case for the new base URL.

**Verify:** `go test ./internal/config/...`

### 2. Config + key management

**File:** `internal/config/settings.go`

- [ ] Add `<Name>KeyEncrypted *string` field to `AppSettings` struct (~line 22).
- [ ] Add `Set<Name>Key`, `Get<Name>Key`, `Has<Name>Key` methods (follow
  Thaura/Moonshot pattern: `Protect`/`Unprotect` + nil-check + `Has`).
  ~40 lines total; copy-paste from Thaura and rename.

**Verify:** `go build ./...`

### 3. Live settings

**File:** `internal/config/live.go`

- [ ] Add `Has<Name>Key() bool` method (~line 97).
- [ ] Add `Get<Name>Key() (*string, error)` method (~line 125).

The `ReasoningEffort` path and `EffortMap` work off the catalog — no
per-provider change needed unless this provider adds configurable models
(see step 7).

**Verify:** `go build ./...`

### 4. Alias routing

**File:** `internal/gateway/alias.go`

- [ ] Add a `Policy<Name> ThinkingPolicy` constant to the `const` block (~line 14).
- [ ] Add entry to `ListAdvertisedModels()` (~line 36): every Cursor alias.
- [ ] Add `case` for each Cursor alias and each real model id in
  `ResolveModel()` (~line 48).
- [ ] If the provider needs custom reasoning_effort validation, add a
  `is<Name>ValidReasoningEffort()` helper function (see `isDeepSeekValidReasoningEffort`).

**File:** `internal/gateway/alias_test.go`

- [ ] Add `TestResolveModel` cases for each alias + real-id.
- [ ] Update `TestListAdvertisedModels` expected count.

**Verify:** `go test ./internal/gateway/... -run TestResolveModel`

### 5. Sanitizer thinking policy

**File:** `internal/gateway/sanitizer.go`

- [ ] Add `case Policy<Name>:` in `applyThinkingPolicy()` (~line 121). Decide:
  - If the provider uses `reasoning_effort` → like `PolicyK3`.
  - If the provider uses `thinking: {type: enabled|disabled}` → like
    `PolicyDeepSeek`.
  - If the provider doesn't support thinking at all → like
    `PolicyThaura` (delete both params).
- [ ] Add `case Policy<Name>:` in `effectiveEffort()` (~line 159) for log
  labels.
- [ ] Add `case Policy<Name>:` in `stripUnsupportedParams()` (~line 198)
  to delete any params the provider doesn't accept.

**File:** `internal/gateway/sanitizer_test.go`

- [ ] Add test cases for the new policy (strip behavior, effort injection).

**Verify:** `go test ./internal/gateway/... -run TestSanitizeRequest`

### 6. Proxy key dispatch

**File:** `internal/gateway/server.go`

- [ ] Add `case config.Provider<Name>:` to `upstreamKey()` (~line 168).
  Call `s.settings.Get<Name>Key(s.cfg.DataRoot)`.

**File:** `internal/gateway/server_test.go`

- [ ] Add test case for the new provider in the key dispatch table.

**Verify:** `go test ./internal/gateway/...`

### 7. Reasoning effort catalog (if the provider supports configurable effort)

**File:** `internal/config/reasoning_effort.go`

- [ ] Add model constant(s) (~line 10).
- [ ] Add `ReasoningEffortSpec` entry(s) in `ReasoningEffortCatalog()` (~line 39).
- [ ] If the provider uses different normalization than DeepSeek, add an
  `is<Name>Model()` switch.

**File:** `internal/config/reasoning_effort_test.go`

- [ ] Add test cases for normalization.

**Verify:** `go test ./internal/config/...`

### 8. Pricing

**File:** `internal/usage/pricing.go`

- [ ] Define a `<name>Rates` struct (match the provider's token tiers:
  cache-hit/input/output, or cache-hit/cache-miss/output, or just
  input/output).
- [ ] Add a `<name>Pricing` map with entries for each model (~line 35).
- [ ] Add `case config.Provider<Name>:` to `EstimateUSD()` (~line 76)
  with the provider-specific price calculation logic.

**File:** `internal/usage/pricing_test.go`

- [ ] Add `TestEstimateUSD` cases for each new model.

**Verify:** `go test ./internal/usage/...`

### 9. CLI: set command

**File:** `internal/cli/setcmd/setcmd.go`

- [ ] Add local `var <name>Key string` (~line 21).
- [ ] Add `--<name>-key` flag (~line 55).
- [ ] Add block in `runSet()` (~line 98) that calls `s.Set<Name>Key(dataRoot, plain)`
  when the flag is set.
- [ ] Add log line with `provider` = `"<name>"`.
- [ ] Update flag listing in the `!anySet` error message (~line 181).

**File:** `internal/cli/setcmd/setcmd_test.go` (if one exists)

**Verify:** `go test ./internal/cli/setcmd/...`

### 10. CLI: init wizard

**File:** `internal/cli/initcmd/initcmd.go`

- [ ] Add `Thaura` → the provider field in `Flags` struct can remain generic
  if you name the field after the provider, or add a new field.
- [ ] Add `--<name>-key` flag array to `NewCmd()`.
- [ ] In `RunSetup()`, after the DeepSeek block (~line 154), add a block to
  prompt for the new provider key (follow the Thaura pattern: optional, only
  when flag is explicitly set, or integrate into the required-key check).
- [ ] Update the `!s.HasMoonshotKey() || !s.HasDeepSeekKey()` guard
  (~line 195) if this provider's key is also required.

**Verify:** `go test ./internal/cli/...`

### 11. CLI: status

**File:** `internal/cli/status/status.go`

- [ ] Add `"has_<name>_key"` to the output map (~line 69).

**Verify:** `go test ./internal/cli/status/...`

### 12. CLI: serve (start)

**File:** `internal/cli/start/serve.go`

- [ ] Add `"has_<name>_key"` to the `slog.Info("gateway starting")` log
  (~line 41).
- [ ] Add `Has<Name>Key` to `usageui.HealthInfo` on start (~line 99).
- [ ] Add key getter closure to `uiSrv.SetKeySource(usageui.KeySource{...})`
  (~line 107).

**Verify:** `go build ./...`

### 13. Doctor / health checks

**File:** `internal/doctor/doctor.go`

- [ ] Add a `<name>_key_present` check block (~line 99) following the
  Thaura pattern.

**File:** `internal/doctor/doctor_test.go`

- [ ] Add test case for the new check.

**Verify:** `go test ./internal/doctor/...`

### 14. Usage UI: server-side

**File:** `internal/usageui/server.go`

- [ ] Add `Has<Name>Key` to `HealthInfo` struct (~line 28).

**File:** `internal/usageui/effort.go`

- [ ] Add `case config.Provider<Name>:` to `providerLabel()` (~line 92).

**File:** `internal/usageui/balance.go`

- [ ] Add `<Name>` field to `BalancesResponse` struct (~line 36).
- [ ] Add `fetch<Name>Balance()` function (follow `fetchMoonshotBalance` or
  `fetchDeepSeekBalance` pattern, depending on the provider's balance API).
- [ ] Add goroutine call in `handleBalances()` to fetch the new provider's
  balance.

**File:** `internal/usageui/server.go`

- [ ] If the provider has special response shape needs (like DeepSeek's
  `pickDeepSeekAmount`), add parsing helpers.

**Verify:** `go build ./internal/usageui/...`

### 15. Usage UI: dashboard HTML/CSS

**File:** `internal/usageui/static/index.html`

This is the largest touchpoint. Search for each `<Name>`-specific location
below and add the new provider entry.

- [ ] **CSS variables** (~line 18): Add `--<name>: #<hex>;` and
  `--<name>-light: #<hex>;`. Choose a distinct color not used by
  moonshot (#7c3aed), deepseek (#2563eb), or thaura (#059669).
- [ ] **Provider CSS classes** (~line 190): Add `.<name> { color: var(--<name>); }`.
- [ ] **Effort provider border** (~line 247): Add `.effort-provider.<name> { border-color: ...; }`.
- [ ] **Effort save button** (~line 293): Add `.effort-save-btn.<name>` +
  `.effort-save-btn.<name>:hover:not(:disabled)` rules.
- [ ] **PROVIDER_COLORS** (line 582): Add `<name>: '#<hex>'`.
- [ ] **PROVIDER_LIGHT** (line 583): Add `<name>: '#<hex>'`.
- [ ] **MODEL_COLORS** (line 589): Add `'<name>::<model-id>': '#<hex>'`
  for each model.
- [ ] **Logo map** (line 612): Add `<name>: '/static/<name>.svg'` (or `png`).
- [ ] **PRICING_URLS** (line 693): Add `<name>: 'https://...docs/pricing'`.
- [ ] **TOPUP_URLS** (line 699): Add `<name>: 'https://...top-up'` if available.
- [ ] **PRICING** object (line 707): Add `<name>:` block with model pricing
  entries (format matches the provider's token tiers).
- [ ] **PROVIDER_NAMES** (line 962): Add `<name>: '<display-name>'`.
- [ ] **EFFORT_DOCS_URLS** (line 964): Add `<name>: 'https://...docs/thinking'`
  if supported.
- [ ] **Provider status grid** (line 903): Add `{ id: '<name>', name: '<Display>', logo: '/static/<name>.svg', ok: h.has_<name>_key }`.
- [ ] **Balance stat rendering** (line 1247): Add `renderBalanceStat('<name>', data.<name>)`
  to the balances HTML builder.
- [ ] **Health API fields**: The `HealthInfo` schema in JS expects
  `has_<name>_key` (snake_case). Ensure the JSON field matches what
  `HealthInfo` serializes.

**Verify:** `go build ./...` (HTML is embedded, so rebuild catches syntax
errors in file paths).

### 16. Logo file

- [ ] Place the provider logo in `.github/img/<name>.svg` (prefer SVG;
  PNG is fine).
- [ ] Ensure the file is committed to the repo (used in README + dashboard).

### 17. Rules files

All under `.cursor/rules/`.

| File | What to add |
| ---- | ----------- |
| `gateway.mdc` (~line 56) | Row in "Primary Cursor aliases" table for each alias |
| `cursor-settings.mdc` (~line 27) | Row(s) in "Model alias table" |
| New file: `<name>.mdc` | Provider-specific rules file. Model table, sanitizer policy, thinking mode, pricing source, color, API key field name, upstream config. Follow `thaura.mdc` as template. |
| `usage.mdc` (~line 19) | Pricing page URL; pricing summary row in provider section |
| `general.mdc` | Add entry in Routing Map + Routing Table for the new provider rule file |

### 18. README

**File:** `README.md`

- [ ] Add provider logo link in the header bar (~line 13), between existing
  logos (right of Thaura, using `&ensp;&middot;&ensp;` separator).
- [ ] Add row in "Prerequisites" table (~line 62) for the new provider key.
- [ ] Add alias row(s) in "Switch providers" table (~line 114).
- [ ] If provider supports configurable effort: add row in reasoning effort
  table (~line 185).
- [ ] Add provider pricing section with table (~line 226) following the
  Moonshot/DeepSeek/Thaura pattern. Include: API model IDs, pricing tiers,
  roles, docs links, top-up link.
- [ ] Add the key flag (`--<name>-key`) to the `discursive set` description
  in the CLI reference (~line 335).

### 19. Tests — comprehensive update

Add test cases for the new provider in every file below:

| Test file | What to add |
| --------- | ----------- |
| `internal/config/urls_test.go` | Base URL + ChatCompletionsURL case |
| `internal/config/reasoning_effort_test.go` | If configurable effort: normalization tests |
| `internal/gateway/alias_test.go` | `TestResolveModel` cases for each alias + real-id |
| `internal/gateway/sanitizer_test.go` | Provider-specific thinking policy + strip behavior |
| `internal/gateway/server_test.go` | `TestModelsListContent`: update expected alias count. Key dispatch test. |
| `internal/gateway/optimizer_test.go` | If the provider should NOT get `prompt_cache_key` injected (verify it's omitted). |
| `internal/usage/pricing_test.go` | `TestEstimateUSD` cases for each model |
| `internal/usageui/server_test.go` | Balance response shape test |
| `internal/usageui/balance_test.go` | New fetch function test |
| `internal/doctor/doctor_test.go` | New key-presence check |
| `internal/cli/setcmd/` (if exists) | Key flag + save test |
| `internal/cli/init_test.go` | Init flow with new key |
| `internal/cli/start_test.go` | Start log output includes new key status |

### 20. Final verification

```bash
go build ./...
go vet ./...
go test ./...
make verify   # if Makefile present
```

If any test or lint fails, fix it before declaring the provider added.

## Tips

- **Use Thaura as the template.** It's the most recently added provider and
  has the cleanest minimal-surface pattern (optional key, no thinking, no
  cache tiers, no configurable effort). Start there and add complexity as
  needed.
- **Order matters.** Do config/urls → settings → alias → sanitizer before
  CLI and UI because the dashboard tests may import the gateway package.
- **Provider-optional vs required.** If the new provider is required (users
  must provide a key), update the validation guard in `initcmd.go` (~line 195)
  and the "Prerequisites" table in README to say `Yes` instead of `No`.
- **Balance check.** If the provider has no balance API, skip adding a
  `fetch<Name>Balance` function and just omit the `<Name>` field from
  `BalancesResponse` — or include it as `{configured: false}`.
- **No thinking support.** If the provider doesn't support any thinking
  params, use `PolicyThaura`'s pattern: `delete(body, "thinking")` +
  `delete(body, "reasoning_effort")`.
- **Usage UI colors.** After picking a hex color for the provider, test it
  in both the dashboard's dark theme and the effort-save buttons for
  readability.
