---
name: open-pr
description: >
    Create a GitHub PR from the current branch in this Discursive Go repo.
    Generates PR description with functional line count, key files to review,
    and non-technical summary from the git diff.   Supports draft mode.
model: deepseek-v4-flash
disable-model-invocation: true
allowed-tools: Bash, Read
---

# /open-pr — Create a GitHub Pull Request

## Call budget

This skill MUST complete in **≤8 total tool calls**:
- Step 0: 1 Bash — batch state gathering
- Step 1: 1 AskQuestion
- Step 2: 1 Bash — dump diff + stats
- Step 3: 1 Read — read the dumped diff once (100ms, tiny)
- Step 4: 2 Bash — `git push` then `gh pr create` (dependent; can't chain a
  heredoc body onto a `&&` push safely)
- Step 5: 1 Bash — cleanup `tmp/diff.diff`
- Step 6: inline report

Every call beyond 8 (and any Bash call that isn't one of the above) is a bug.
The prior session made ~15 redundant Shell calls (multiple `git status`/`git
log`/`git branch`, per-file `git diff`, `git fetch`, `gh pr view`). None of
those are allowed in this flow.

## Flow (strict)

### Step 0: gather state (1 batch Bash call)

With `working_directory` set to the repo root:

```bash
git branch -vv && git branch -r && git log -15 --oneline
```

From this output, identify:
- Current branch name
- Remote base branch options (pick `main`, plus any other active branches)
- Recent commits (the branch's story)

**Do NOT run `git status`, `git log`, or `git branch` separately.**

### Step 1: ask base branch (AskQuestion)

**Always** present a choice of base branches derived from step 0. Include `main`
and any other visible active branches plus an "Other" option. Never default.

### Step 2: dump functional diff + stats (1 Bash call)

Dump to gitignored `./tmp/diff.diff`, plus `--shortstat` and `--stat`:

```bash
git diff <BASE>...HEAD -- ':(exclude)*.md' ':(exclude)*.mdc' ':(exclude)**/test/**' ':(exclude)**/*_test.go' ':(exclude)**/*.txt' ':(exclude).cursor/rules/**' ':(exclude).cursor/skills/**' ':(exclude)examples/**' ':(exclude)VERSION' > ./tmp/diff.diff && echo "---SHORTSTAT---" && git diff <BASE>...HEAD --shortstat -- ':(exclude)*.md' ':(exclude)*.mdc' ':(exclude)**/test/**' ':(exclude)**/*_test.go' ':(exclude)**/*.txt' ':(exclude).cursor/rules/**' ':(exclude).cursor/skills/**' ':(exclude)examples/**' ':(exclude)VERSION' && echo "---STAT---" && git diff <BASE>...HEAD --stat -- ':(exclude)*.md' ':(exclude)*.mdc' ':(exclude)**/test/**' ':(exclude)**/*_test.go' ':(exclude)**/*.txt' ':(exclude).cursor/rules/**' ':(exclude).cursor/skills/**' ':(exclude)examples/**' ':(exclude)VERSION' && echo "---NONFUNCSTAT---" && git diff <BASE>...HEAD --shortstat -- '*.md' '*.mdc' '.cursor/skills/**' '*.txt'
```

If the functional diff is empty (zero lines, or only binary `usage.db` type
junk), fall back to the full diff (no pathspec) and treat as a rules/docs-only
PR per that section below.

### Step 3: read the diff ONCE (1 Read call)

Read `./tmp/diff.diff` with the Read tool. Derive from it:

- **Summary bullets (4–6):** what changed and why. Each 1–2 lines. Do not
  enumerate markdown/rules/test file changes here.
- **Key files to review:** pick 3–8 from the `--stat` in step 2 (largest first).

Prefer this single Read over many per-file `git diff` calls.

### Step 4: push then create PR (2 Bash calls)

Push the branch:

```bash
git push -u origin HEAD
```

If the branch is already pushed (no-op), that's fine — `gh pr create` still
works. If push fails for a non-obvious reason (not "Everything up-to-date"),
surface the error and stop.

Then create the PR (body assembled from step 3's read):

```bash
gh pr create --base <BASE> --title "<short imperative description>" --body "$(cat <<'EOF'
**Functional lines changed:** <N> files, +<I> −<D>

## Key files to review
- `…` — …
- `…` — …

## Summary
- **…**: …
- **…**: …

## Non-functional changes  # only if substantial rules/skills/README changes
- `…` — …
EOF
)"
```

For draft PRs, add `--draft`. Capture the PR URL from the output. The
`## Non-functional changes` section is included only when step 2's
`---NONFUNCSTAT---` shows substantial changed files (e.g. README + rules large
enough to matter to reviewers). Keep it brief.

### Step 5: cleanup (1 Bash call)

```bash
rm -f ./tmp/diff.diff
```

### Step 6: report (inline, no call)

Report the PR URL captured from step 4's `gh pr create` output. Done. Do not
`gh pr view` after creation unless the PR appeared to fail.

## PR body order (always)

1. `**Functional lines changed:** …`
2. `## Key files to review`
3. Optional `## TODO_IN_THIS_PR`
4. `## Summary`
5. Optional `## Non-functional changes`

## PR title format

```
<short imperative description>
```

No JIRA key, no `[WIP]` prefix. For draft PRs, use `--draft`.

Examples:
- `Add smart router with content-based model downgrade`
- `Fix usage query time-window handling`

## Rules-only / docs-only PRs (zero functional files)

When the functional diff is empty:

- `**Functional lines changed:**` → `0` explicitly.
- `## Key files to review` → 3–8 paths from the **full** diff (largest first).
- `## Summary` → describe the actual changed files.

## Anti-patterns (do NOT do these)

- Do NOT read individual file diffs with `git diff -- <file>` — use the single
  temp-file dump instead.
- Do NOT run `git status` — the branch state is from step 0.
- Do NOT run `git fetch` as a separate call — if needed, prepend to step 0.
- Do NOT run `gh pr view` after creation unless step 4 output is suspicious.
- Do NOT run more than one Bash call for independent commands — chain with `&&`.
- Do NOT forget `working_directory` — every Bash call that touches the repo
  needs it set explicitly.
