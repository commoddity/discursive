---
name: audit-rules-and-docs
description: >
    Full audit of all .cursor/rules/*.mdc files, .cursor/skills/*/SKILL.md
    files, and the README.md in the Discursive repo against the current
    codebase. Verifies file paths, Go package/symbol references, skill
    frontmatter, and README staleness (broken references, outdated
    commands/structure). Read-only — never auto-removes content; all changes
    require human approval.
model: deepseek-v4-flash
disable-model-invocation: true
paths: .cursor/**
allowed-tools: Bash, Read, Grep, Glob, WebFetch
---

# /audit-rules-and-docs — Full rules + skills + README audit for the Discursive repo

You are auditing ALL `.cursor/rules/*.mdc` rule files,
`.cursor/skills/*/SKILL.md` skill files, AND the root `README.md` in the
Discursive repo for mechanical accuracy against the live codebase. This is a
**verification pass**, not a cleanup pass.

**This skill is scoped to the `discursive/` repo** — a Go CLI/daemon local
OpenAI-compatible gateway (see `general.mdc`). It runs from the repo root as the
working directory.

---

## CRITICAL CONSTRAINTS

1. **NEVER auto-remove entries.** Even if a referenced file/function is gone,
   the principle behind the entry may still apply. Flag it; let the human
   decide.
2. **NEVER judge whether a problem class "still applies."** Most bugs are
   latent.
3. **ONLY flag mechanical staleness**: broken file paths, deleted Go packages,
   removed symbol names, entries older than 6 months with no verification
   timestamp update.
4. **DO flag contradictions**: two entries prescribing different fixes for the
   same symptom pattern.
5. **DO flag gaps**: new packages/commands not in the routing tables, skills
   missing frontmatter fields, rules missing verification timestamps.
6. **DO flag skill frontmatter issues**: missing `description`, skills that
   should have `paths` scoping but don't, skills that should have
   `disable-model-invocation: true` but don't.
7. **DO flag README staleness**: broken file references, outdated commands, CLI
   subcommands or flags no longer reflected in the root `README.md`.

## Audit scope

- `.cursor/rules/*.mdc` — all domain rule files
- `.cursor/skills/*/SKILL.md` — all skill files
- `README.md` — the repo's single root README (there is one top-level README)

---

## Phase 1 — Inventory

### Rules inventory

Read every `.cursor/rules/*.mdc` file. For each, extract:

- Every file path reference
- Every package/command name referenced in tables or prose
- Every function/export/identifier name referenced in code blocks
- Every `<!-- last-verified: YYYY-MM -->` timestamp
- Every problem class entry (symptom/cause/fix table)

### Skills inventory

Read every `.cursor/skills/*/SKILL.md`. For each, extract:

- Frontmatter fields present and their values
- Every file path or glob referenced in the skill body
- Every package/symbol/command name referenced
- Whether it has `description` (should be present)
- Whether it has `disable-model-invocation` (should be `true` for manual-only
  workflows)
- Whether it has `paths` (should be set for skills that auto-activate on
  specific file types)
- Whether it has `allowed-tools` (should match the skill's actual tool usage)

### README inventory

Read the root `README.md`. Extract:

- Title / first heading (identifies what the README covers)
- Category: `custom` (project-specific content), `pointer` (delegates to another
  doc), `auto-generated` (tool-generated stub)
- File paths referenced (e.g., `internal/gateway/`, `.cursor/rules/`)
- Commands referenced (e.g., `go build ./...`, `go test ./...`, `make verify`)
- CLI subcommands / flags referenced (e.g., `discursive start`, `discursive
  doctor`)
- Architecture claims (e.g., "host Go CLI/daemon", provider routing via alias)
- Dependencies/tools mentioned (e.g., "uses cloudflared tunnel", "uses Cobra")
- Setup / running instructions

Build three audit inventories: rules, skills, and README.

---

## Phase 2 — Mechanical verification

### Rules

#### File paths

- Verify each referenced file exists in the codebase
- If gone: note that it's gone

#### Package / command names

- Check package reference tables for completeness (e.g., `internal/gateway/`,
  `internal/usage/`)
- Flag any package NOT in the table

#### Function/export names

- `grep -r` to verify each referenced Go identifier/export
- Flag missing ones

#### Timestamps

- Entries with `last-verified` older than 6 months: flag for review
- No `last-verified` timestamp: flag (all entries should have one)

### Skills

#### Frontmatter completeness

Check each skill for:

| Field                      | Expectation                                                           |
| -------------------------- | --------------------------------------------------------------------- |
| `name`                     | Should match directory name (no leading `/`)                          |
| `description`              | Must be present, should describe when to auto-invoke                  |
| `disable-model-invocation` | `true` for manual-only skills, `false`/absent for auto-invoked skills |
| `paths`                    | Should be set for skills that apply to specific file types            |
| `allowed-tools`            | Should match the actual tools the skill uses                          |

Flag any missing or mismatched fields.

#### Skill body references

- Verify referenced file paths exist
- Verify referenced Go packages/symbols/commands exist
- Check for stale references to `.claude/rules/` or `.claude/skills/` (should be
  `.cursor/rules/` and `.cursor/skills/` now)
- Check for stale references to `.claude/commands/` or `.cursor/commands/` (dead
  format)

### READMEs

#### Broken references

- Verify every file path referenced in the root `README.md` exists
- If gone: note the reference and whether the content it pointed to still exists
  elsewhere

#### Outdated commands / structure

- Cross-reference README commands against the actual Makefile / Go build targets
  (e.g., `make verify`, `go build ./...`, `go test ./...`)
- Cross-reference CLI subcommands/flags against the Cobra command tree in
  `internal/cli/`
- If the README references a subcommand, flag, or default port/alias that no
  longer matches: flag
- Flag any `examples/` or `.cursor/rules/*.mdc` paths that no longer exist

#### Missing identity / architecture

Flag the root `README.md` if it lacks:

- Product identity: what is this, what does it do?
- Architecture: how does it fit together (gateway, tunnel, usage, config)?
- Setup / running instructions: how to get it running locally?
- Key files: what are the important entry points?

**Exception:** A README that intentionally delegates to another doc is
intentional — don't flag it as missing identity, but DO verify the target
exists.

---

## Phase 3 — Structural audit

### Rule file completeness

- Go package reference table vs actual `internal/` packages
- CLI command coverage vs actual Cobra commands under `internal/cli/`
- Routing table in `general.mdc` vs actual `.cursor/rules/*.mdc` files

### Skill scope audit

For each skill, evaluate whether its scoping is appropriate:

| Check              | Detail                                                                            |
| ------------------ | --------------------------------------------------------------------------------- |
| Manual vs auto     | Should Cursor auto-invoke this, or only on direct `/command`?                     |
| File scope         | If the skill only applies to certain files, does it have `paths` set?             |
| Tool restrictions  | If the skill is read-only, does it have `allowed-tools` restricting writes?       |
| Duplicate coverage | Does any rule file cover the same domain as a skill? Should they be consolidated? |

### Routing coverage

- For each rule file, verify its domain is listed in the routing table in
  `general.mdc`
- For each skill, verify it's documented somewhere (routing table or SKILL.md
  description)
- Flag any rule file or skill not referenced

### README coverage

- The repo has a single top-level `README.md`; verify it reflects the actual
  repo structure documented in `general.mdc` (projects, packages, providers,
  build/run commands).
- Flag the root README if it documents packages, commands, or providers that no
  longer exist, or omits ones that do.
- For any pointer-like README content, verify the target exists.

---

## Phase 4 — Contradiction scan

For each problem class entry, scan all other entries across ALL rule files,
skills, AND READMEs:

- Same symptom + different prescribed fix = CONTRADICTION
- Flag with both entries cited, let human resolve
- Boundary conditions (e.g., "Rule A unless condition B") are NOT contradictions

Also check README claims against rule file claims:

- If the README says a provider is supported (e.g., Moonshot Kimi) but the rules
  say it's been removed/disabled: flag
- If the README gives a default pipe/port/tunnel mode but `general.mdc` says
  otherwise: flag

---

## Phase 5 — Gap analysis

Identify common codebase patterns NOT covered by any rule, skill, or README:

- New `internal/` packages added since last audit
- New CLI commands/providers/aliases without documentation
- New patterns not captured in rules
- Skills that could be created for common workflows
- Root README sections that need updating

---

## Output format

```
## Audit — YYYY-MM-DD

### Rules: Mechanical verification

| Severity | File | Reference | Issue |
|----------|------|-----------|-------|
| BROKEN | file.mdc:42 | `path/to/deleted` | File does not exist |
| STALE | file.mdc:150 | last-verified: 2025-08 | Not verified in 11 months |
| MISSING | general.mdc | some-file.mdc | Routing table references file not in .cursor/rules/ |

### Skills: Frontmatter audit

| Skill | Field | Issue |
|-------|-------|-------|
| skill-name | paths | Missing — should scope to specific file types |
| skill-name | allowed-tools | Missing — skill is read-only, should restrict writes |

### Skills: Mechanical verification

| Severity | Skill | Reference | Issue |
|----------|-------|-----------|-------|
| BROKEN | skill-name | `.claude/skills/` | References old .claude path |

### README: Mechanical verification

| Severity | README | Reference | Issue |
|----------|--------|-----------|-------|
| BROKEN | README.md:15 | `internal/deleted/` | Referenced package does not exist |
| STALE | README.md:8 | `discursive start --provider kimi` | That subcommand/flag no longer exists |
| STALE | README.md:3 | port 4001 | Default port changed to 4002 |

### Structural audit

#### Undocumented packages
- `internal/newpkg/` — not in rule file table

#### Undocumented CLI commands / providers
- `discursive newcmd` — not in rule file hierarchy

#### Routing table missing entries
- `some-file.mdc` exists in .cursor/rules/ but not in general.mdc routing table

#### README coverage gaps
- README omits `internal/newpkg/` (recently added package with no docs)

### Contradictions
- (none) / [Entry A in X] vs [Entry B in Y] — same symptom, different fixes
- README says default provider is Moonshot but general.mdc routing table says plain
  alias switching drives provider selection

### Gaps
- New pattern: [description] — consider adding to [rule file]
- New skill candidate: [description] — common workflow without automation
- README section to add: [description] — documents a new command/package

### Summary
- Rules BROKEN: N | STALE: N | MISSING: N
- Skills BROKEN: N | MISSING_FRONTMATTER: N
- README BROKEN_REFS: N | STALE: N
- Contradictions: N
- Gaps: N
```
