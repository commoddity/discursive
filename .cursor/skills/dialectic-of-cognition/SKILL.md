---
name: dialectic-of-cognition
description: >
  Capture session learnings into project rules.
  Invoke manually at end of session; also from /task-3-complete. Never auto-trigger.
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /dialectic-of-cognition — Capture session learnings into evolving rules

You are performing rule maintenance per the **Rule Maintenance (Self-Evolving
Rules)** section of `.cursor/rules/general.mdc`. Read that section in full
before proceeding — it is the authoritative process. What follows is the
operational harness; the rules file defines the principles.

**Store**: write only to `.cursor/rules/*.mdc`. Never create or edit another rules directory.

This command has two modes. Run A and/or B as applicable.

---

## Mode A — Debugging learnings

### Phase A0 — Triage

Triggers: debugging >5 min; external docs; multiple corrective attempts;
non-obvious root cause.

If none: state "Mode A: no debugging triggers — skipping." Move to Mode B.

### Phase A1 — Extract (Particular → General)

Symptom → root-cause **class** → resolution **pattern**. Discard one-offs.

### Phase A2 — Route

Use the Routing Table in `general.mdc` to pick spoke files for **this** product.

---

## Mode B — Code change → rule impact

### B0 — Summarize what changed

### B1–B2 — Route and read matching spokes

### B3 — Abort gate

Can you state the rule without naming a specific file/function/class/variable/endpoint?
If not, skip (value stays in the diff).

### B4 — Mini-audit: indices drift check

**ALWAYS run this phase.** Even when Mode A found nothing and Mode B has no
learnings to encode, this check runs. It catches drift on any changed file, not
just the files that triggered learnings.

Use the **changed-file manifest** supplied by the caller rather than scanning
the git diff (do NOT invoke Git commands). The manifest lists the files added,
deleted, renamed, or edited this session. Verify that `general.mdc`'s indices
are consistent with that manifest and the filesystem:

1. **Rules index check:** `ls .cursor/rules/*.mdc` vs the Rules index table in
   `general.mdc`. Every `.mdc` file must have a row. No row should reference a
   file that doesn't exist.

2. **Skills index check:** `ls .cursor/skills/*/SKILL.md` vs the Skills index
   table in `general.mdc`. Every skill directory must have a row. No row should
   reference a skill that doesn't exist.

3. **Routing table check:** Every entry in the Rules index and Skills index must
   have a corresponding row (or combined row) in the Routing table.

4. **Cross-reference:** If any manifest entry added, removed, renamed, or
   modified a rule file or skill file, `general.mdc` MUST be checked even if the
   indices drift check finds nothing — the file content may have changed (new
   domains, changed scopes).

If the caller did not supply a changed-file manifest, ASK the user for it before
proceeding. Do not fall back to running `git diff` or any other Git command.

If drift is found, update `general.mdc` immediately. This is NOT optional. The
indices are the single source of truth for all rule/skill routing.

### B5 — Encode into `.cursor/rules/*.mdc`

Prefer refine existing symptom tables. Add `<!-- last-verified: YYYY-MM -->`.

**Rule writing style — LLM-optimized, not human-prose:**

Rules are consumed by LLMs, not humans reading documentation. Every word must
earn its place:

- **Active voice, imperative mood.** "Use `style` not `bg` for hex" not
  "Developers should consider using..."
- **No preamble, no fluff.** Drop introductory sentences. Lead with the
  actionable pattern.
- **Code over prose.** A code snippet shows the pattern; 3 sentences of
  explanation is too many.
- **One idea per bullet.** Scan-able. No paragraph of exposition.
- **Delete hedge words.** "may", "sometimes", "in some cases" → state the rule.
  Exceptions can be noted tersely.
- **File size matters.** Prefer shorter. If a rule can be said in 2 lines
  instead of 6, use 2.

Example — BAD (human prose):

> When proxying Cursor requests through the gateway, you should sanitize
> the tool schemas before forwarding because some providers reject
> schemas that include unsupported JSON Schema constructs.

GOOD (LLM-optimized):

> Strip unsupported JSON Schema constructs before proxying:
>
> ```go
> sanitizeToolSchema(schema) // drops: $ref, allOf, anyOf, oneOf
> ```

---

## Shared integrity

1. Contradiction check
2. File size >550 lines → propose split (do not split without approval)
3. Decay: flag entries older than 6 months in touched project spokes
4. Verification: would a cold-read AI recognize the symptom and apply the fix
   without re-investigation?

---

## Output format

```
## Session capture — [topic]

### Mode A — Debugging learnings
…

### Mode B — Code change → rule impact
…

### Phase B4 — Mini-audit: indices drift check
- Rules index: consistent / [drift found]
- Skills index: consistent / [drift found]
- Routing table: consistent / [drift found]
- Cross-reference (changed rule/skill files): none / [files requiring general.mdc update]

### Learnings encoded
| Mode | Rule file | Problem class / Section | Action |
| ---- | --------- | ----------------------- | ------ |
| … | … | … | … |

### Integrity
- Contradictions: none / [describe]
- File size flags: none / [file] at [N] lines
- Stale entries flagged: none / [list]
- Verification: [pass / gaps found]

### Skipped
- …
```

If A/B both find nothing: **"Nothing to capture — session was routine."**
