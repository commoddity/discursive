---
name: audit-rules
description: >
  Full audit of all .cursor/rules/*.mdc and .claude/skills/*/SKILL.md files
  against the current codebase. Verifies paths, package names, skill
  frontmatter, and routing completeness. Read-only — never auto-removes content;
  all changes require human approval.
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob
---

# /audit-rules — Full rules + skills audit against current codebase

You are auditing ALL `.cursor/rules/*.mdc` rule files AND `.claude/skills/*/SKILL.md`
skill files for mechanical accuracy against the live codebase. This is a
**verification pass**, not a cleanup pass.

**Principles**: `.cursor/rules/general.mdc`
**Store**: `.cursor/rules/*.mdc` only — never `.claude/rules/`

## CRITICAL CONSTRAINTS

1. **NEVER auto-remove entries.** Flag; let the human decide.
2. **NEVER judge whether a problem class "still applies."**
3. **ONLY flag mechanical staleness**: broken paths, missing exports, entries
   older than 6 months without verification timestamp update.
4. **DO flag contradictions** and **gaps**.
5. **DO flag skill frontmatter issues**.
6. **Scaffolding awareness**: until phase tasks land app source
   (`planning/phases/INDEX.md` T01+), many `cmd/` / `internal/` paths will not
   exist yet. Flag those as `PENDING_SCAFFOLD`, not `BROKEN`. Reference:
   `examples/use-kimi-on-cursor/` (may be present). Sequence of record is
   `planning/phases/INDEX.md`.

---

## Phase 1 — Inventory

### Rules

For each `.cursor/rules/*.mdc`: paths, package names, problem-class tables,
`last-verified` timestamps.

### Skills

For each `.claude/skills/*/SKILL.md`: frontmatter, path refs, no `.claude/rules/`.

---

## Phase 2 — Mechanical verification

- Verify referenced product paths exist or are `PENDING_SCAFFOLD`
- Compare `internal/` tree in `go.mdc` vs actual packages when scaffolded
- Grep for exports cited in rules
- Timestamp decay >6 months

### Skills frontmatter

| Field | Expectation |
|-------|-------------|
| `name` | Matches directory |
| `description` | Present |
| `disable-model-invocation` | `true` for manual workflows |
| `allowed-tools` | Match actual usage |

---

## Phase 3 — Structural audit

- Routing Map + table in `general.mdc` vs actual `.cursor/rules/*.mdc`
- Skills inventory in `general.mdc` vs `.claude/skills/*/`
- `CLAUDE.md` must be symlink → `.cursor/rules/general.mdc`
- `.claude/rules/` must not exist
- Expected spokes: `general`, `go`, `cobra`, `kimi`, `deepseek`, `gateway`,
  `cursor-settings`, `tunnel`, `unix-packaging`, `usage`
- `/task-3-complete` must document push-by-default, `--no-push`, and Manual test
  / Nothing to test handoff (flag dilution as `MISSING`)
- `/task-2-execute` and `/task-3-complete` must document **verify tooling presence**
  hard-abort (Makefile `verify`, lint config, lefthook); root tooling must exist
  or flag `BROKEN`
- No leftover Wails/Vue/inspect-css (or other deleted-stack) references.
  **DeepSeek is a first-class provider** — do not flag `deepseek.mdc` or
  DeepSeek product content as leftover.

---

## Phase 4 — Contradiction scan

Same symptom + different fix across rules/skills → CONTRADICTION.

---

## Phase 5 — Gap analysis

New packages (gateway, tunnel, usage) missing from rules;
missing skills for common workflows.

---

## Output format

Use the standard audit tables (BROKEN / PENDING_SCAFFOLD / STALE / MISSING)
plus Summary counts. Include Layout: CLAUDE.md symlink OK/BROKEN.
