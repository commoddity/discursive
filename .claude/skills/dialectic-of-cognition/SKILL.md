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

**Store**: write only to `.cursor/rules/*.mdc`. Never create or edit
`.claude/rules/`.

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

### B4 — Encode into `.cursor/rules/*.mdc`

Prefer refine existing symptom tables. Add `<!-- last-verified: YYYY-MM -->`.

---

## Shared integrity

1. Contradiction check
2. File size >550 lines → propose split (do not split without approval)
3. Decay: flag entries older than 6 months in touched project spokes

---

## Output format

```
## Session capture — [topic]

### Mode A — Debugging learnings
…

### Mode B — Code change → rule impact
…

### Learnings encoded
| Mode | Rule file | Problem class / Section | Action |
| ---- | --------- | ----------------------- | ------ |
| … | … | … | … |

### Integrity
- …

### Skipped
- …
```

If A/B both find nothing: **"Nothing to capture — session was routine."**
