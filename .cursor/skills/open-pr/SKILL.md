---
name: open-pr
description: >
    Create a GitHub PR from the current branch in this Discursive Go repo.
    Generates PR description with functional line count, key files to review,
    and non-technical summary from the git diff. Supports draft mode.
disable-model-invocation: true
allowed-tools: Bash, Read
---

# /open-pr — Create a GitHub Pull Request

**Canonical skill file:** `SKILL.md` (this file).

Read and follow that file in full. This entry exists so Cursor discovers the
skill under `.cursor/skills/`.

## Repo context

This is the **Discursive** local gateway repo (Go). PRs for this repo are
personal — they are not tied to a JIRA / ticket tracker, and there is no
`[WIP]` prefix convention. Keep titles plain-descriptive. This repo's
`/task-3-complete` skill does **not** open PRs (it commits and pushes only), so
this skill is invoked standalone: **always ask the user which branch to target
as the PR base. Never assume or default to a base branch without asking.**

## PR title (required format)

```
<description>
```

- `<description>` is a short imperative summary of the change (e.g. "Add smart
  router with content-based model downgrade").
- **No JIRA key, no `[WIP]` prefix** — this repo uses neither.
- For draft PRs, use GitHub's native `draft` flag instead of any title prefix.

Examples:

- `Add smart router with content-based model downgrade`
- `Fix usage query time-window handling`
- `Bump dependencies and clean up release config`

## PR body order (required)

1. `**Functional lines changed:** …` — from functional diff with pathspec
   `:(exclude)*.md` `:(exclude)*.mdc` `:(exclude)**/test/**`
   `:(exclude)**/*_test.go` `:(exclude)**/*.txt` plus doc/rule exclusions
   (this repo's docs live in `.cursor/rules/`, `.cursor/skills/`, and reference
   code lives in `examples/`):
   `:(exclude).cursor/rules/**` `:(exclude).cursor/skills/**`
   `:(exclude)examples/**` `:(exclude)VERSION`
2. `## Key files to review` — 3–8 curated paths from functional `--stat`
3. Optional `## TODO_IN_THIS_PR`
4. `## Summary` — short bullet-point list of what changed and why. Derive from
   the functional diff (step 1). Keep each bullet to 1–2 lines. Do not
   enumerate markdown, rules, or test file changes here. For a CLI/gateway
   change the summary already covers the behavioral change; no separate
   "what the user sees" section is needed.
5. Optional `## Non-functional changes` — only when markdown, rules, or test
   file changes are large enough to be worth highlighting. Keep it brief.

### Rules-only / docs-only PRs (zero functional files)

If (and only if) the functional diff is empty — e.g. the PR touches only
`.md`/`.mdc`, rules, or test files:

- `**Functional lines changed:**` → report `0` explicitly.
- `## Key files to review` → fall back to the most relevant 3–8 paths from the
  **full** diff (largest changed files first), since there is no functional
  `--stat` to draw from.
- The `## Summary` (and the `## Non-functional changes` section) must describe
  the actual changed files, because there is no functional diff to summarize.
  Keep bullets to 1–2 lines each.
- The existing 3–8 functional-path requirement for `## Key files to review`
  still applies in full whenever functional files are present.

Do **not** apply this fallback when the functional diff is non-empty — keep the
functional files as the source for Key files and Summary.
