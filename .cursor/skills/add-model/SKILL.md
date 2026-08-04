---
name: add-model
description: >-
  Adds a new Moonshot/Kimi or DeepSeek model to the Discursive gateway
  end-to-end. Starts from model name, cursor alias, and provider docs page;
  walks every Go source, rules file, README section, usage UI touchpoint,
  and test that must change. Does not support adding new providers (Thaura
  or net-new) — only models within existing Moonshot/DeepSeek providers.
  Manual-only — /add-model.
disable-model-invocation: true
---

# /add-model — Add a new Moonshot or DeepSeek model

Adds a **model from an existing provider** (Moonshot/Kimi or DeepSeek).
Thaura and net-new providers are out of scope (use `/task-1-plan` instead).

What you need from the user:

| Required | Description |
| -------- | ----------- |
| Model name | Real model id, e.g. `kimi-k3.5` or `deepseek-v4-fast` |
| Cursor alias (OpenAI) | What Cursor sees, e.g. `gpt-5-high-unlimited` |
| Docs page from provider | Model-specific docs URL for pricing/parameters |
| Maybe: pricing | USD/MTok cache-hit, input, output rates |

## Steps

Work in this exact order. Stop at each verify gate — do not advance on
broken test/build.

### 1. Reasoning effort catalog

**File:** `internal/config/reasoning_effort.go`

- [ ] Add model constant to `const` block (~line 10).
- [ ] Add `ReasoningEffortSpec` entry in `ReasoningEffortCatalog()` (~line 39)
  with Model, Provider, Label, Options, Default. Match the existing shape
  (K3 uses `low|high|max`; K2.6 uses `off|on`; DeepSeek uses `off|high|max`).
- [ ] If DeepSeek: add constant to `isDeepSeekModel()` chain (~line 118).

**File:** `internal/config/reasoning_effort_test.go`

- [ ] Add test cases in `TestNormalizeReasoningEffort` for the new model's
  valid and invalid effort values.

**Verify:** `go test ./internal/config/...`

### 2. Model route map

**File:** `internal/gateway/alias.go`

- [ ] Add `ThinkingPolicy` constant if new policy shape needed. Usually a new
  Kimi model reuses `PolicyK3` or `PolicyK2`; a new DeepSeek model reuses
  `PolicyDeepSeek`.
- [ ] Add to `ListAdvertisedModels()` (cursor alias line).
- [ ] Add to `ResolveModel()` switch — both the cursor alias→route case **and**
  the real model id→route case.

**Verify:** `go test ./internal/gateway/... -run TestResolveModel`

### 3. Sanitizer thinking policy

**File:** `internal/gateway/sanitizer.go`

- [ ] Add `case Policy*` block in `applyThinkingPolicy()` (~line 119). Follow
  the pattern of the policy you're reusing (e.g., `PolicyK3` uses top-level
  `reasoning_effort`; `PolicyK2` uses `thinking: {type: enabled|disabled}`).
- [ ] Add case in `effectiveEffort()` (~line 159) to extract the effort string
  for logs.
- [ ] Add case in `stripUnsupportedParams()` (~line 198) if needed to delete
  provider-incompatible thinking params.

**Verify:** `go test ./internal/gateway/... -run TestSanitizeRequest`

### 4. Pricing

**File:** `internal/usage/pricing.go`

- [ ] Add entry to `moonshotPricing` or `deepseekPricing` map. Use live provider
  pricing page.

**File:** `internal/usageui/static/index.html`

- [ ] Add entry to `MODEL_COLORS` object (~line 589): `'provider::modelid': '#hex'`.
- [ ] Add entry to `PRICING` object (~line 707) under the correct provider key.

**Verify:** `go test ./internal/usage/...`

### 5. Rules files

All under `.cursor/rules/`.

| File | What to add |
| ---- | ----------- |
| `gateway.mdc` (~line 56) | Row in "Primary Cursor aliases" table |
| `cursor-settings.mdc` (~line 27) | Row in "Model alias table" |
| `kimi.mdc` (~line 14) OR `deepseek.mdc` (~line 19) | Row in "Supported models" table |
| `usage.mdc` (~line 19) | Pricing page URL row; pricing summary line in provider section |

### 6. README

**File:** `README.md`

- [ ] Add alias row in "Switch providers" table (~line 112).
- [ ] If model supports configurable effort: add row in reasoning effort table (~line 183).
- [ ] Add pricing row in provider pricing table: Moonshot (~line 200) or DeepSeek (~line 220).

### 7. Tests

Add test cases for the new model in every file below:

| Test file | What to add |
| --------- | ----------- |
| `internal/gateway/alias_test.go` | `TestResolveModel`: alias + real-id cases. `TestListAdvertisedModels`: expect new alias. |
| `internal/gateway/sanitizer_test.go` | Thinking policy test matching the model's effort shape. |
| `internal/gateway/server_test.go` | `TestModelsListContent`: update `len(payload.Data)` count and expected alias list if the new model adds an advertised alias. |
| `internal/gateway/optimizer_test.go` | If DeepSeek: verify the new model doesn't get `prompt_cache_key` injected (add test case). |
| `internal/usage/pricing_test.go` | `TestEstimateUSD` case with token counts → expected cost. |

### 8. Final verification

```bash
go build ./...
go vet ./...
go test ./...
make verify   # if Makefile present
```

If any test or lint fails, fix it before declaring the model added.

## What does NOT change (within-provider model only)

- `internal/config/urls.go` — base URLs are per-provider, not per-model.
- `internal/config/settings.go` / `internal/config/live.go` — key fields are per-provider.
- `internal/config/tunnel.go` — same.
- `internal/gateway/optimizer.go` — cache-key injection is provider-level (`route.Provider == ProviderMoonshot`); new models on same provider inherit.
- `internal/gateway/proxy.go` — upstream key/URL dispatch is provider-based.
- `internal/usageui/server.go` / `balance.go` — health/balance are per-provider.
- `internal/cli/setcmd/`, `initcmd/`, `start/run.go`, `status/`, `doctor/` — all per-provider.
- `internal/usageui/effort.go` — `providerLabel()` only has provider cases, not model cases.
- `PROVIDER_COLORS`, `PROVIDER_LIGHT`, `PRICING_URLS`, `TOPUP_URLS` in `index.html` — per-provider.
