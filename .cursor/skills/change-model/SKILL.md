---
name: change-model
description: >-
  Retargets an existing Cursor model alias to a different real model from the
  same or another already-supported provider (e.g. point `gpt-4.1-turbo` from
  `glm-5.2` to `glm-5.3`). Walks every Go source, rules file, README section,
  usage UI touchpoint, and test that must be renamed or revalidated, and checks
  for behavioral changes (thinking/reasoning_effort shape, pricing, alias map,
  advertised models). REQUIRES the provider's docs page for the NEW model.
  Manual-only — /change-model. Does not add net-new providers (use /add-provider).
model: deepseek-v4-pro
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /change-model — Retarget a Cursor alias to a new model

Updates an **existing Cursor alias** so it points at a different real model id
(e.g. `gpt-4.1-turbo` → `glm-5.2` becomes `gpt-4.1-turbo` → `glm-5.3`). The
alias string **stays the same**; the model it resolves to changes. Covers the
whole plumbing surface and — critically — every behavioral delta the swap may
introduce (thinking on/off support, reasoning_effort values, pricing, advertised
model list).

This is **not** a plain find/replace. Read the new model's docs first; the old
and new models rarely differ by only a name.

What you need from the user:

| Required | Description |
| -------- | ----------- |
| Cursor alias | The alias that stays fixed, e.g. `gpt-4.1-turbo` |
| Old real model | What the alias currently resolves to, e.g. `glm-5.2` |
| New real model | What the alias should resolve to, e.g. `glm-5.3` |
| Provider | The provider both models belong to (or the new provider if crossing) |
| **Docs page for the NEW model** | Provider-specific docs URL — REQUIRED to compare thinking/effort/pricing vs the old model |

Also confirm from the new model's docs page:

| Behavioral axis | Reason |
| --------------- | ------ |
| Does it still support `thinking.type: "disabled"`? | If not, the sanitizer must never emit it — map off → minimum effort instead |
| What `reasoning_effort` values exist (`low/high/max`, etc.)? | Update the catalog Options + normalize; the UI effort selector changes |
| Per-token pricing (input / cached / output)? | Refresh pricing maps + JS + tests. If the provider has **not published** a rate, keep the old model's numbers and mark them PROVISIONAL rather than inventing them |
| Is the model API live, or "coming soon"? | If not live, still update plumbing but flag it to the user |
| Same provider base URL? | If the new model is on a different provider, that's a larger cross-provider change — escalate |

## Steps

Work in this exact order. Stop at each verify gate — do not advance on broken
test/build.

### 0. Establish old→new behavior delta

| File | What to do |
| ---- | ---------- |
| Docs | Read the new model's docs page. Diff its thinking/effort model and pricing against the old model. Write down what changed (e.g. "no more `disabled`", "effort is now `low/high/max` only", "price not published") |

**Verify:** You can state, in one bullet per axis, exactly what changes. If you
cannot, re-read the docs before touching code.

### 1. Reasoning effort catalog

**File:** `internal/config/reasoning_effort.go`

- [ ] Rename the model constant (e.g. `ModelZaiGLM52` → `ModelZaiGLM53`) and its
  value string.
- [ ] Update the `ReasoningEffortSpec` entry in `ReasoningEffortCatalog()`:
  - `Options` to the new model's supported effort values.
  - `Default` to the value you want (match the old model's UX where sensible;
    always-thinks models default to minimum effort for cost, e.g. `"low"`).
- [ ] Update the comment above the catalog citing the new model's docs URL.
- [ ] Update `isZaiModel()` / the provider model switch if it references the
  renamed constant.
- [ ] If the new model maps effort differently in `normalizeZaiEffort()` (e.g.
  it no longer accepts `off`), update that normalization and its error message.

**File:** `internal/config/reasoning_effort_test.go`

- [ ] Update `TestNormalizeReasoningEffort` cases: rename model, replace
  old-effort expectations (e.g. `off → off` becomes `off → low` if disabled is
  unsupported). Add cases for any newly valid/invalid values.
- [ ] Update `TestNormalizeReasoningEffortMapDefaults` for the new default.

**Verify:** `go test ./internal/config/...`

### 2. Model route map

**File:** `internal/gateway/alias.go`

- [ ] Rename the real-id entry in `ListAdvertisedModels()` (old model id → new).
- [ ] In `ResolveModel()`: update the **alias→route** case(s) to the new real
  model id, and update any **real model id→route** case.
- [ ] Keep the same `ThinkingPolicy` unless the swap changes the thinking shape
  (e.g. old model was policy A supporting off, new model always thinks → it may
  need a different code branch; see step 3).

**Verify:** `go test ./internal/gateway/... -run TestResolveModel`

### 3. Sanitizer thinking policy

**File:** `internal/gateway/sanitizer.go`

- [ ] Update every literal/comment referencing the old model id.
- [ ] If the new model's thinking shape differs (the common case), rework the
  `applyThinkingPolicy()` branch for it:
  - Thinking **still on/off** → keep the same enabled/disabled shape.
  - **Always thinks** (`disabled` unsupported) → set `thinking: {type:
    "enabled"}` unconditionally and always inject `reasoning_effort`; map
    off/empty → minimum effort (e.g. `"low"`). Never emit `disabled`.
- [ ] If only one model keeps an old behavior (e.g. `glm-4.7` still supports
  `disabled`), guard that branch by a real-model-id check so the two coexist.
- [ ] Update `effectiveEffort()` default if the model can no longer return `off`.
- [ ] Check `internal/config/urls.go` comments mentioning the model's
  thinking/effort shape.

**Verify:** `go test ./internal/gateway/... -run TestSanitizeRequest` (substring-match picks up the `TestSanitizeRequest_*` cases)

### 4. Pricing

| File | What to do |
| ---- | ---------- |
| `internal/usage/pricing.go` | Rename the model key to the new id. If the provider published a new rate, use it. If **no rate is published yet**, keep the old model's numbers and add a `PROVISIONAL` comment pointing at the provider's pricing page (do NOT invent rates). |
| `internal/usageui/static/index.html` | In `MODEL_COLORS`: rename `'provider::<old>'` → `'provider::<new>'`. In `PRICING`: rename the entry key and set values (add the same PROVISIONAL comment if unpublished). |

**Verify:** `go test ./internal/usage/...` and `go build ./...` (HTML is
embedded).

### 5. Rules files

All under `.cursor/rules/`. Search each for the **old** model id and rename to
the new one, updating the surrounding intent (thinking mode, effort options,
description):

| File | What to update |
| ---- | -------------- |
| `general.mdc` | Provider shorthand list (`Z.AI (\`glm-5.3\`, …)`) if it names the model |
| `gateway.mdc` | "Primary Cursor aliases" rows + any compat-alias rows + downgrade/dubbing rows that name the model |
| `cursor-settings.mdc` | "Model alias table" rows + any prose notes mentioning the model |
| `<provider>.mdc` (e.g. `zai.mdc`) | Model table, pricing table, sanitizer policy, reasoning-effort normalization, symptoms. **Add a symptom** for the new model's most likely failure mode (e.g. `thinking: disabled` breaking) if the shape changed |
| `usage.mdc` | Provider pricing section + a PROVISIONAL note if the new rate is unpublished |

### 6. README

**File:** `README.md`

- [ ] Rename the alias row in the "Switch providers" table.
- [ ] Rename the reasoning-effort table row (Options + Default) — remove any
  now-invalid `off` option if the model always thinks.
- [ ] Rename the provider pricing table row (old model id → new) and add a
  PROVISIONAL note if the rate is unpublished.
- [ ] Update the parameter matrix row (thinking / reasoning_effort columns).

### 7. Tests

Rename and revalidate every test that exercised the **old** model:

| Test file | What to do |
| --------- | ----------- |
| `internal/gateway/alias_test.go` | `TestResolveModel`: rename expected model in alias + real-id cases. `TestListAdvertisedModels`: count unchanged unless you added/dropped a model. |
| `internal/gateway/sanitizer_test.go` | `TestSanitizeRequest` effort table: rename model + update expectations for the new thinking shape (e.g. `off` → `enabled` + `low`). Any old-model-off test that no longer applies must be rewritten. |
| `internal/gateway/router_test.go` | Subagent-router downgrade test naming the model as a fallback candidate. |
| `internal/gateway/verbosity/controller_test.go` | If it used the old id as an "unknown model" fixture, swap to the new id. |
| `internal/usage/pricing_test.go` | `TestEstimateUSD`: rename `zai_glm*` cases + model ids. |
| `internal/cli/usage/confirm_test.go` | Model id used as a Z.AI fixture row. |
| `internal/config/reasoning_effort_test.go` | See step 1. |

**Verify:** `go test ./...`

### 8. Final verification

```bash
go build ./...
go vet ./...
go test ./...
make verify   # if Makefile present
```

If any test or lint fails, fix it before declaring the model changed.

## What NOT to change (same-provider model swap)

- `internal/config/urls.go` — base URLs are per-provider, not per-model (only a
  comment mentioning the model's thinking shape may need editing).
- `internal/config/settings.go` / `live.go` — key fields are per-provider.
- `internal/gateway/proxy.go` / `optimizer.go` — upstream key/URL dispatch and
  cache-key injection are provider-based.
- `internal/gateway/server.go` / `vision/` — `glm-4.6v` vision worker is a
  separate model, not the one you're swapping.
- `internal/usageui/server.go` / `balance.go` — health/balance are per-provider.
- `PROVIDER_COLORS`, `PROVIDER_LIGHT`, `PRICING_URLS`, `TOPUP_URLS`,
  `PROVIDER_NAMES` in `index.html` — per-provider, not per-model.

## Tips

- **Docs first.** Never start a rename before reading the NEW model's docs page
  and confirming the thinking/effort/pricing delta. The user is required to
  supply this URL.
- **Not a blind replace.** `off`→`off` thinking can silently break if the new
  model no longer supports `disabled`. Always re-derive the policy branch from
  the docs, then reconcile the tests that asserted the old shape.
- **Unpublished pricing.** If the rate card doesn't list the new model, carry the
  old model's numbers forward and flag them `PROVISIONAL` in Go + JS + rules +
  README, each pointing at the provider's pricing page. Update the tests so they
  pin whatever value you carry. Do not invent a number.
- **Keep the old model resolvable?** If the old model id should still work as a
  real-id request, leave its `ResolveModel` case in place pointing at the old
  model — but drop it from `ListAdvertisedModels` if it's no longer advertised.
  Ask the user whether old ids should remain accepted.
- **Compatibility aliases.** If Cursor rewrites the alias client-side (e.g.
  `gpt-4.1-turbo` → `gpt-4-turbo`), update that compat alias → new model too so
  both names resolve consistently.
