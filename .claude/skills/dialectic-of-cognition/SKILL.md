---
name: dialectic-of-cognition
description: >
  Capture session learnings into project rules; optionally sync only extremely
  generalizable PROCESS principles into the Turboplan pack (PoC feedback).
  Invoke manually at end of session; also from /task-3-complete. Never auto-trigger.
disable-model-invocation: true
allowed-tools: Bash, Read, Grep, Glob, Edit, Write
---

# /dialectic-of-cognition — Capture session learnings into evolving rules

You are performing rule maintenance per the **Rule Maintenance (Self-Evolving
Rules)** section of `.cursor/rules/general.mdc`. Read that section in full
before proceeding — it is the authoritative process. What follows is the
operational harness; the rules file defines the principles.

**Store (this project)**: write only to `.cursor/rules/*.mdc`. Never create or
edit `.claude/rules/`.

**Store (Turboplan pack — Mode C only)**: see Mode C. Extremely high bar.
Process methodology only.

This command has three modes. Run A and/or B as applicable, then **always**
run Mode C triage (usually a quick skip).

---

## Mode A — Debugging learnings (this project)

### Phase A0 — Triage

Triggers: debugging >5 min; external docs; multiple corrective attempts;
non-obvious root cause.

If none: state "Mode A: no debugging triggers — skipping." Move to Mode B.

### Phase A1 — Extract (Particular → General)

Symptom → root-cause **class** → resolution **pattern**. Discard one-offs.

### Phase A2 — Route

Use the Routing Table in `general.mdc` to pick spoke files for **this** product.

---

## Mode B — Code change → rule impact (this project)

### B0 — Summarize what changed

### B1–B2 — Route and read matching spokes

### B3 — Abort gate (project rules)

Can you state the rule without naming a specific file/function/class/variable/endpoint?
If not, skip for project rules (value stays in the diff).

### B4 — Encode into `.cursor/rules/*.mdc`

Prefer refine existing symptom tables. Add `<!-- last-verified: YYYY-MM -->`.

---

## Mode C — Turboplan process feedback (PoC → methodology pack)

**Purpose:** This project is a proof of concept for [Turboplan](https://github.com/).
When implementing it teaches something about the **methodology itself** (how to
bootstrap, slice phases, plan/execute/complete, maintain rules, audit), feed that
back into the Turboplan pack — **not** product engineering knowledge.

### C0 — Locate Turboplan

1. If env `TURBOPLAN_ROOT` is set and exists → use it  
2. Else if sibling `../turboplan` exists (relative to this repo root) → use it  
3. Else: **"Mode C: Turboplan pack not found — skipped."** Stop Mode C.

### C1 — Extremely high bar (ALL must pass)

A candidate may sync to Turboplan **only if every** check passes:

| # | Gate | Fail if… |
| - | ---- | -------- |
| 1 | **Process-only** | About product domain: APIs, models, vendors, protocols, UI frameworks, languages, packaging for *this* app, pricing, tunnels-as-product, IDE-specific payloads, etc. |
| 2 | **Universal** | Would not help an arbitrary unrelated Turboplan project (e.g. a CRUD web app with no LLM) |
| 3 | **Nameless** | Cannot be stated without naming this product, a vendor, a model id, a specific framework, or a concrete file/endpoint |
| 4 | **Methodology-shaped** | Does not improve how agents: adapt rules, seed/order phases, plan, execute, complete, dialectic, audit, verify layers, or manage INDEX/branches |
| 5 | **Non-duplicative** | Already stated clearly in Turboplan `METHODOLOGY.md` / guides / templates — skip or tiny refine only |

**When in doubt: skip.** Prefer under-syncing. Domain lessons stay in **this**
repo’s `.cursor/rules/` only (Modes A/B).

**Examples that FAIL Mode C (encode locally if useful, never Turboplan):**

- Sanitizer order, Kimi/DeepSeek params, Cloudflare Quick Tunnel ops
- Gateway package layout, `slog` field lists
- "Public HTTPS because Cursor cloud SSRF" as a *product* rule (spoke-local OK)

**Examples that PASS Mode C (process):**

- Delete wrong-stack rules instead of leaving landmines when retargeting a fork  
- INDEX Status uses `✅`, not the word `Done` in the INDEX column  
- Stop at failed verification — do not advance the next layer  
- Downstream stubs get Reality notes on complete; do not invent scope  
- Third-party **display/alias ids** in operator UIs may change behavior — keep
  proven ids as default until verified (stated without naming a vendor)

### C2 — Where to write in Turboplan

Prefer **refine** over add. Allowed targets only:

| Target | When |
| ------ | ---- |
| `METHODOLOGY.md` | Pillars, sequential contract, rule-maintenance process, behavioral defaults |
| `PROCESS-FEEDBACK.md` | Short dated log of accepted PoC → pack principles (append-only bullets) |
| `guides/04-dialectic-and-audit.md` | Dialectic/audit procedure improvements |
| `guides/01-adapt-rules-from-goal.md` / `02-seed-phases.md` / `03-run-the-loop.md` | Bootstrap/loop process only |
| `templates/skills/*.SKILL.md` | Procedure changes that every Turboplan project should inherit |
| `templates/rules/general.mdc` | Process sections only (hub→spoke, dialectic, safety *shape*) — never paste product architecture |

**Never** write into Turboplan:

- Never write into Turboplan:
- Secrets, keys, personal paths beyond documenting `TURBOPLAN_ROOT`  

Do **not** commit in the Turboplan repo unless the user explicitly asks; leave
edits in the working tree and report them in the output.

### C3 — Encode

If something passes C1: refine the chosen Turboplan file; append one line to
`PROCESS-FEEDBACK.md`:

```markdown
- YYYY-MM-DD — <one-sentence process principle> (from PoC session: <topic>)
```

If nothing passes: **"Mode C: nothing process-universal — skipped."**

---

## Shared integrity (Modes A/B; and C if edited)

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

### Learnings encoded (this project)
| Mode | Rule file | Problem class / Section | Action |
| ---- | --------- | ----------------------- | ------ |
| … | … | … | … |

### Mode C — Turboplan process feedback
- Pack path: … / not found
- Candidates considered: N
- Accepted: … / none
- Files touched in Turboplan: … / none
- Skipped (too specific): …

### Integrity
- …

### Skipped
- …
```

If A/B/C all find nothing: **"Nothing to capture — session was routine."**
