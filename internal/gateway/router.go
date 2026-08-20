package gateway

import (
	"log/slog"
	"regexp"
	"strings"
)

// SubAgentRouter classifies every chat-completion request and, when the
// request looks like subagent-like cheap work (simple lookup, code search,
// structured extraction, automation), overrides the model to a cheaper
// alternative. Complex work (editing, reasoning) keeps the original model.
//
// Classification uses the content of the last user message — subagents send
// independent chat-completion requests, so each operation is classified on
// its own merits. This is NOT heuristic subagent detection (which proved
// infeasible: Cursor subagent and main-agent HTTP requests are identical);
// it is content-based "what does this turn ask for?" classification.
//
// Routing decisions:
//   - Simple lookup / explanation → downgrade to flash.
//   - Code search / exploration → downgrade to flash.
//   - Structured extraction / json_object → downgrade to flash.
//   - Automation / workflows → downgrade to flash.
//   - Editing / refactoring → keep model.
//   - Complex reasoning / architecture → keep model.
//   - Unclassified / unknown → keep model (conservative default).

// RequestClass labels the inferred task type for logging and tuning.
type RequestClass string

const (
	ClassSubagent             RequestClass = "subagent"
	ClassSimpleLookup         RequestClass = "simple_lookup"
	ClassCodeSearch           RequestClass = "code_search"
	ClassEditing              RequestClass = "editing"
	ClassComplexReasoning     RequestClass = "complex_reasoning"
	ClassStructuredExtraction RequestClass = "structured_extraction"
	ClassAutomation           RequestClass = "automation"
	ClassUnknown              RequestClass = "unknown"
)

// SubAgentRouterConfig controls subagent-routing behavior.
type SubAgentRouterConfig struct {
	// Enabled gates the entire feature. When false, ClassifyAndOverride is a no-op.
	Enabled bool

	// PickDowngradeModel is invoked when a downgrade would be applied. It
	// receives the candidate override model (defaultFlashModel) and the
	// request id for logging, and returns the model to actually use. Use it
	// to steer downgrades into a capacity-aware lane (e.g. glm-4.7 while
	// slots are free, overflow to another model otherwise). Nil = use the
	// candidate as-is. The returned release func must be called when the
	// request completes (non-nil only when a lane slot was taken).
	PickDowngradeModel func(candidate, requestID string) (model string, release func())
}

// ClassifierResult contains the classification outcome for logging.
type ClassifierResult struct {
	RequestClass      RequestClass `json:"request_class"`
	SysPromptLen      int          `json:"sys_prompt_len"`
	MsgCount          int          `json:"msg_count"`
	HasTools          bool         `json:"has_tools"`
	OriginalModel     string       `json:"original_model,omitempty"`
	OverrideModel     string       `json:"override_model,omitempty"`
	OverrideApplied   bool         `json:"override_applied"`
	OverrideReason    string       `json:"override_reason,omitempty"`
	ReleaseLaneSlot   func()       `json:"-"` // frees the downgrade lane slot when set
	ClassificationAge string       `json:"classification_age,omitempty"`
}

// defaultFlashModel is the single downgrade target for all cheap-class traffic.
// All subagent/router downgrades and lane-full overflows land on OpenRouter's
// DeepSeek flash model, regardless of the originally requested model.
const defaultFlashModel = "deepseek/deepseek-v4-flash-0731"

// SubAgentRouter performs request classification and optional model override.
type SubAgentRouter struct {
	cfg SubAgentRouterConfig
}

// NewSubAgentRouter creates a subagent router with the given config.
func NewSubAgentRouter(cfg SubAgentRouterConfig) *SubAgentRouter {
	return &SubAgentRouter{cfg: cfg}
}

// ClassifyAndOverride examines the already-sanitized request body, classifies
// the request type via content-based inspection, and optionally overrides the
// model to a cheaper alternative.
//
// It returns a ClassifierResult for logging (always populated, even when
// disabled) so the caller can introspect classification quality in shadow mode.
//
// The body must have already been through SanitizeRequest (model resolved to
// real model ID, not alias).
func (r *SubAgentRouter) ClassifyAndOverride(body map[string]any, requestID string) ClassifierResult {
	result := ClassifierResult{
		ClassificationAge: "v1",
		RequestClass:      ClassUnknown,
	}

	if body == nil {
		return result
	}

	// Extract signals from the request body.
	result.SysPromptLen = systemPromptLength(body)
	result.MsgCount = messageCount(body)
	result.HasTools = hasTools(body)
	result.OriginalModel = stringField(body, "model")

	// Content-based classification.
	result.RequestClass = classifyRequest(body)

	// Always log classification at DEBUG so operators can tune thresholds.
	lastMsg := lastUserMessage(body)
	lastMsgStripped := stripCursorNoise(lastMsg)
	slog.Debug("subagent_router: classify",
		"request_id", requestID,
		"request_class", result.RequestClass,
		"sys_prompt_len", result.SysPromptLen,
		"msg_count", result.MsgCount,
		"has_tools", result.HasTools,
		"model", result.OriginalModel,
		"msg_roles", messageRoles(body),
		"last_msg_len", len(lastMsg),
		"stripped_len", len(lastMsgStripped),
		"stripped_preview", truncate(lastMsgStripped, 120),
	)

	if !r.cfg.Enabled {
		return result
	}

	// Determine whether to downgrade based on request class.
	if !shouldDowngrade(result.RequestClass) {
		return result
	}

	// Cheap-class traffic always uses the single OpenRouter flash target.
	override := defaultFlashModel

	// Capacity-aware lane selection: let the server steer glm-4.7 downgrades
	// between the plan lane and an overflow model.
	var release func()
	if r.cfg.PickDowngradeModel != nil {
		chosen, rel := r.cfg.PickDowngradeModel(override, requestID)
		if chosen != "" {
			override = chosen
			release = rel
		}
	}

	// No-op if the resolved override equals the current model.
	if override == result.OriginalModel {
		if release != nil {
			release()
		}
		return result
	}

	result.OverrideModel = override
	result.OverrideApplied = true
	result.OverrideReason = string(result.RequestClass) + " downgrade (" + result.OriginalModel + " → " + override + ")"
	result.ReleaseLaneSlot = release
	body["model"] = override
	slog.Info("subagent_router: model_overridden",
		"request_id", requestID,
		"request_class", result.RequestClass,
		"from", result.OriginalModel,
		"to", override,
		"sys_prompt_len", result.SysPromptLen,
		"msg_count", result.MsgCount,
		"has_tools", result.HasTools,
	)

	return result
}

// shouldDowngrade returns true when the request class is safe to run on a
// cheaper model. Conservative classes (editing, complex reasoning) keep their
// original model.
func shouldDowngrade(c RequestClass) bool {
	switch c {
	case ClassSubagent, ClassSimpleLookup, ClassCodeSearch, ClassStructuredExtraction, ClassAutomation:
		return true
	case ClassEditing, ClassComplexReasoning, ClassUnknown:
		return false
	default:
		return false
	}
}

// thinkingClass returns true when a request class benefits from thinking being
// enabled (hard reasoning / real code edits). Downgrade-safe mechanical
// classes and unknown return false so the thinking-effort coupling can keep
// cheap turns fast.
func thinkingClass(c RequestClass) bool {
	switch c {
	case ClassEditing, ClassComplexReasoning:
		return true
	default:
		return false
	}
}

// classifyRequest inspects the last user message and request structure to
// infer the task type. Returns ClassUnknown when no clear signal is found.
//
// Classification priority (higher matches first):
//  1. Structured extraction (response_format json_object / json_schema)
//  2. Summarization (summarize, condense, capture conversation — deterministic
//     text synthesis, routes to flash)
//  3. Automation / workflows (git, PR, branch — mechanical orchestration)
//  4. Complex reasoning (keyword/prefix signals; long-message heuristic WITH
//     exploration guard — long+detailed exploration prompts skip the heuristic)
//  5. Code search (find, search, explore — before editing; narrow keywords to
//     avoid false positives from incidental mentions like "the search function")
//  6. Editing / refactoring (modify, edit, write — after code search)
//  7. Simple lookup (question-like, explanation)
//  8. Catch-all: everything else → unknown
//
// Lint tasks (fix + lint/remove unused/rename) are a sub-case of automation:
// when the message contains both editing keywords AND lint keywords, the
// lint signal wins (mechanical, flash handles it). When the message contains
// strong editing keywords (refactor, rewrite, implement) alongside
// automation keywords, editing wins (creative work needs pro).
//
// Classification operates on the stripped message (Cursor XML/summary
// blocks removed) so we classify on the actual prompt, not the wrapper.
//
// Exploration guard: long messages (≥400 chars, ≥3 sentences) that match
// code-search keywords but NOT explicit complex-reasoning signals (keywords
// like "trade-off", "system design", or prefixes like "how would you")
// are NOT classified as complex reasoning. This prevents exploration subagent
// prompts from hitting the long-message heuristic and instead lets them fall
// through to code search (which fires before editing).
func classifyRequest(body map[string]any) RequestClass {
	lastMsg := lastUserMessage(body)
	// Strip Cursor-injected XML blocks (open_and_recently_viewed_files,
	// attached_files, code_selection, etc.) and conversation-summary blocks
	// so we classify on the actual user prompt, not the Cursor wrapper.
	stripped := stripCursorNoise(lastMsg)

	// If the last user message is entirely Cursor boilerplate (XML +
	// conversation summary), we have nothing real to classify on. Return
	// unknown (conservative: keep current model).
	if stripped == "" {
		return ClassUnknown
	}

	lower := strings.ToLower(stripped)

	// Raw (untrimmed) content of the last user message, including Cursor's
	// attached code_selection / terminal_selection parts. Used to recover a
	// lint/CI signal that lives in the attachment (not the laconic prompt).
	raw := rawUserMessageContent(body)

	// Structured extraction: response_format json_object / json_schema.
	// This is independent of message content and takes highest priority.
	if hasStructuredOutput(body) {
		return ClassStructuredExtraction
	}

	// Summarization / synthesis: summarizing conversations, writing structured
	// summaries, condensing content — deterministic text-synthesis tasks that
	// flash handles well. Checked early so they route to flash even when the
	// prompt contains editing keywords like "write" or "capture".
	if isSummarization(lower) {
		return ClassSimpleLookup // treated as simple lookup → flash
	}

	// Automation / workflow: git commands, PR creation, branch operations,
	// scripts — deterministic tool-orchestration tasks that flash handles well.
	// Checked before editing so "create a PR" routes to automation.
	// When both automation AND strong-editing keywords are present, editing wins:
	// "refactor and create a PR" needs pro for the refactoring part.
	// Exception: lint/linter tasks are mechanical (remove unused imports, rename variables, fix formatting) —
	// even when the message contains "fix", flash handles them fine.
	if isAutomation(lower) {
		if isLintTask(lower) || !isStrongEditing(lower) {
			return ClassAutomation
		}
	}

	// Complex reasoning: long message with multiple requirements, architecture,
	// planning, design strategy. Checked before editing so "create a plan" /
	// "design the architecture" keep the model rather than being caught by the
	// broad "create" / "design" editing keywords.
	if isComplexReasoning(lower, stripped) {
		return ClassComplexReasoning
	}

	// Code search / exploration: message asks to search, find, explore.
	// Checked before editing so exploration prompts that incidentally use
	// editing words (e.g. "for adding a new endpoint") still route to
	// code search. Narrow keywords (phrases, not single ambiguous words)
	// prevent false positives from editing tasks.
	if isCodeSearch(lower) {
		return ClassCodeSearch
	}

	// Mechanical fix: a laconic fix-family prompt ("fix all these") backed by
	// attached lint/CI output (code_selection with eslint/astro-lint/
	// golangci-lint, terminal_selection showing "exit status 1", "no-undef",
	// pre-commit) is a deterministic cleanup — flash handles it, so treat it as
	// automation rather than editing. Real code edits ("fix the nil pointer
	// dereference") carry no such markers and still route to editing below.
	if isEditing(lower) && isMechanicalLintSignal(raw) {
		return ClassAutomation
	}

	// Editing / refactoring: message mentions editing, modifying, refactoring.
	// isEditing fires after code search so exploration prompts that contain
	// incidental editing words are still caught by code search.
	if isEditing(lower) {
		return ClassEditing
	}

	// Simple lookup: question-like message, explanation request, or short
	// ambiguous prompt with no stronger signal. This is the catch-all for
	// cheap-to-route queries.
	if isSimpleLookup(lower, stripped) {
		return ClassSimpleLookup
	}

	return ClassUnknown
}

// hasStructuredOutput returns true if the request body specifies structured
// output (json_object / json_schema response_format).
func hasStructuredOutput(body map[string]any) bool {
	rf, ok := body["response_format"].(map[string]any)
	if !ok {
		return false
	}
	typ, _ := rf["type"].(string)
	return typ == "json_object" || typ == "json_schema"
}

// isSummarization returns true when the message asks to summarize, condense,
// or synthesize existing content into structured output. These are
// deterministic text-synthesis tasks that flash handles well.
// Detects phrases like "capture the conversation into a summary",
// "summarize this chat", "condense the following", etc.
func isSummarization(lower string) bool {
	summarizeKeywords := []string{
		"summarize", "summarise", "summary of",
		"capture the", "condense the", "condense this", "condense following",
		"synthesize the", "synthesise the",
	}
	for _, kw := range summarizeKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isSimpleLookup returns true when the message looks like a quick lookup or
// explanation that doesn't need code modification.
func isSimpleLookup(lower string, _ string) bool {
	simplePrefixes := []string{
		"what is", "what does", "what are", "what's", "whats",
		"how do i", "how does", "how is", "how are",
		"explain", "describe", "tell me", "show me",
		"why is", "why does", "why are",
		"can you explain", "can you tell", "please explain",
		"who is", "where is", "when does",
	}
	for _, p := range simplePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// isCodeSearch returns true when the message asks to search, find, explore,
// or locate code without modifying it. Keywords are intentionally narrow
// (multi-word phrases or unambiguous single words) to avoid false positives
// from incidental mentions (e.g. "fix the search function" is editing, not
// code search).
func isCodeSearch(lower string) bool {
	searchKeywords := []string{
		"search for", "search the", "search this",
		"find all", "find the", "find out", "find usages",
		"locate the", "locate all",
		"look for", "look up",
		"grep", "explore", "discover",
		"what files",
		"where is the", "where are the",
		"show me the",
		"list all", "list the", "list files",
		"scan the", "scan for",
		"inspect the",
	}
	for _, kw := range searchKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isEditing returns true when the message asks to modify, edit, refactor,
// or write code.
func isEditing(lower string) bool {
	editKeywords := []string{
		"refactor", "rewrite", "implement", "create",
		"modify", "change", "update", "edit",
		"add", "remove", "delete", "replace",
		"fix", "patch", "correct", "resolve",
		"generate", "write", "build", "make a",
		"convert", "migrate", "rename", "move",
	}
	for _, kw := range editKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isStrongEditing returns true when the message contains keywords that
// unambiguously indicate creative code work that needs pro quality.
// These always take priority over automation classification.
func isStrongEditing(lower string) bool {
	strongKeywords := []string{
		"refactor", "rewrite", "implement",
		"design", "architecture",
	}
	for _, kw := range strongKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isAutomation returns true when the message asks for deterministic
// tool-orchestration tasks (git, PR, branch, scripts, lint) that flash
// handles well.
func isAutomation(lower string) bool {
	autoKeywords := []string{
		"create a pr", "create pr", "open a pr",
		"push", "commit", "git push", "git commit",
		"run the tests", "run ci", "deploy",
		"create a branch", "checkout",
		"lint", "linter", "linting",
		"merge", "rebase", "cherry-pick",
	}
	for _, kw := range autoKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isLintTask returns true when the message is about linting/static-analysis
// cleanup. Even when the message contains "fix", a linter task is mechanical
// (remove unused imports, rename variables, fix formatting) — flash handles
// it fine.
func isLintTask(lower string) bool {
	lintKeywords := []string{
		"lint", "linter", "linting",
		"unused", "remove unused", "unused import",
		"eslint", "golangci-lint", "golangci",
		"format", "formatting",
	}
	for _, kw := range lintKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isMechanicalLintSignal returns true when the attached message content (the
// raw, unstripped last-user-message including Cursor's code_selection and
// terminal_selection parts) carries strong lint / static-analysis / CI
// markers. Cursor strips these attachments from the visible prompt, so a
// laconic prompt like "fix all these" loses the "lint" evidence unless we
// scan the raw content. Used to reclassify fix prompts with lint attachments
// as automation (flash) while real code edits stay on pro.
//
// Markers name the *mechanisms* that produce mechanical cleanup work:
// specific linter tool names and their common error output, plus pre-commit
// / formatting tooling. A lone "fix" word in a code edit ("fix the nil
// pointer dereference in proxy.go") matches none of these.
func isMechanicalLintSignal(raw string) bool {
	markers := []string{
		"eslint", "astro-lint", "astro-format",
		"golangci-lint", "golangci", "stylelint",
		"prettier", "lefthook", "pre-commit",
		"no-undef",
		"exit status", "check diagnostics",
		"lint", "linting", "linter",
		"formatting", "format errors",
	}
	r := strings.ToLower(raw)
	for _, m := range markers {
		if strings.Contains(r, m) {
			return true
		}
	}
	// "format" alone is too broad (appears in many non-lint contexts), but
	// when it appears near the word "check" it signals the astro-format /
	// prettier-style pre-commit gate.
	if strings.Contains(r, "format") && strings.Contains(r, "check") {
		return true
	}
	return false
}

// isComplexReasoning returns true when the message requires architectural or
// deep reasoning that benefits from pro-level models. Triggers on both prefix
// keywords and message-length+pattern heuristics.
func isComplexReasoning(lower, stripped string) bool {
	complexPrefixes := []string{
		"design a ", "architect ", "plan a ",
		"how would you ", "what is the best way ",
		"evaluate ", "analyze ", "compare ",
	}
	for _, p := range complexPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	complexKeywords := []string{
		"trade-off", "tradeoff", "pros and cons",
		"best practice", "design pattern",
		"system design", "data model", "schema design",
		"scalability", "performance bottleneck",
	}
	for _, kw := range complexKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// Long messages (≥400 chars after stripping) with multiple sentences
	// suggest complex multi-part reasoning — BUT we guard against exploration
	// prompts (messages that look like code search without explicit complex
	// signals). Long exploration prompts from subagents (300-800 chars with
	// numbered requirements like "explore: 1. ... 2. ...") would otherwise
	// hit this heuristic and stay on pro when flash handles them fine.
	if len(stripped) >= 400 {
		sentences := strings.Count(stripped, ".") + strings.Count(stripped, "?") + strings.Count(stripped, "!")
		if sentences >= 3 && !isCodeSearch(lower) {
			return true
		}
	}

	return false
}

// ----- Utility helpers ------------------------------------------------

// truncate returns s truncated to maxLen characters with "…" suffix.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// lastUserMessage returns the content string of the last message with role
// "user", or "" if none found.
func lastUserMessage(body map[string]any) string {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		// Handle array-of-parts content (Cursor multi-part format).
		if parts, ok := msg["content"].([]any); ok {
			var sb strings.Builder
			for _, p := range parts {
				if pmap, ok := p.(map[string]any); ok {
					if text, ok := pmap["text"].(string); ok {
						sb.WriteString(text)
					}
				}
			}
			return sb.String()
		}
		// Handle plain string content.
		if s, ok := msg["content"].(string); ok {
			return s
		}
		return ""
	}
	return ""
}

// rawUserMessageContent returns the full, untrimmed text of the last user
// message, including Cursor's attached code_selection / terminal_selection
// parts (which carry lint/CI evidence that the visible <user_query> prompt
// omits). Unlike lastUserMessage / stripCursorNoise, this does not extract
// only the <user_query> inner text — the entire part content is kept so
// attachment signals survive for isMechanicalLintSignal.
func rawUserMessageContent(body map[string]any) string {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		if parts, ok := msg["content"].([]any); ok {
			var sb strings.Builder
			for _, p := range parts {
				pmap, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := pmap["text"].(string); ok {
					sb.WriteString(text)
					sb.WriteString(" ")
				}
			}
			return sb.String()
		}
		if s, ok := msg["content"].(string); ok {
			return s
		}
		return ""
	}
	return ""
}
func messageRoles(body map[string]any) string {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return ""
	}
	roles := make([]string, 0, len(msgs))
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		roles = append(roles, role)
	}
	return strings.Join(roles, ", ")
}

// systemPromptLength returns the length (in chars) of the first system or
// developer message content, or 0 if none found.
func systemPromptLength(body map[string]any) int {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return 0
	}
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		if s, ok := msg["content"].(string); ok {
			return len(s)
		}
		if parts, ok := msg["content"].([]any); ok {
			return len(parts) // rough: count parts
		}
	}
	return 0
}

// messageCount returns the number of messages in the messages array, or 0
// if missing or malformed.
func messageCount(body map[string]any) int {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return 0
	}
	return len(msgs)
}

// hasTools returns true if the body declares a non-empty tools array.
func hasTools(body map[string]any) bool {
	tools, ok := body["tools"].([]any)
	if !ok {
		return false
	}
	return len(tools) > 0
}

// ------ Cursor content stripping ------

// xmlBlock matches the full Cursor-injected XML blocks:
//
//	<open_and_recently_viewed_files>…</open_and_recently_viewed_files>
//	<attached_files>…</attached_files>
//	<code_selection>…</code_selection>
//	<system_reminder>…</system_reminder>
//	<system_notification>…</system_notification>
//	<timestamp>…</timestamp>
//	<user_query>…</user_query>                     (opening tag — content is kept)
//	</user_query>                                  (closing tag)
//	<terminal_files_information>…</terminal_files_information>
var xmlBlock = regexp.MustCompile(
	`(?s)<(?:open_and_recently_viewed_files|attached_files|code_selection|system_reminder|system_notification|timestamp|user_query|/user_query|terminal_files_information)\b[^>]*>.*?</(?:open_and_recently_viewed_files|attached_files|code_selection|system_reminder|system_notification|timestamp|user_query|terminal_files_information)>`,
)

// summaryBlock matches Cursor conversation-summary blocks:
//
//	[Previous conversation summary]:…\n\nSummary:\n…
var summaryBlock = regexp.MustCompile(`(?s)\[Previous conversation summary\].*`)

// stripCursorNoise removes Cursor-injected XML blocks and conversation
// summaries from the message content, extracting only the user's actual
// prompt.
func stripCursorNoise(msg string) string {
	// Step 1: Extract <user_query> content (if present).
	if idx := strings.Index(msg, "<user_query>"); idx >= 0 {
		end := strings.Index(msg, "</user_query>")
		if end > idx {
			msg = strings.TrimSpace(msg[idx+len("<user_query>") : end])
		}
	}

	// Step 2: Strip conversation summary blocks.
	msg = summaryBlock.ReplaceAllString(msg, "")

	// Step 3: Strip remaining Cursor XML blocks.
	msg = xmlBlock.ReplaceAllString(msg, "")

	// Step 4: Collapse whitespace and trim.
	msg = strings.Join(strings.Fields(msg), " ")

	return strings.TrimSpace(msg)
}
