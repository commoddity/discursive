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
	ClassificationAge string       `json:"classification_age,omitempty"`
}

// modelOverrideMap maps expensive models to cheaper equivalents for
// subagent downgrades. Entries here take priority. Models not in this
// map fall back to defaultFlashModel.
var modelOverrideMap = map[string]string{
	"deepseek-v4-pro": "deepseek-v4-flash",
}

// defaultFlashModel is the fallback model for any traffic that triggers a
// downgrade but whose original model isn't in modelOverrideMap.
const defaultFlashModel = "deepseek-v4-flash"

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

	// Apply model override: prefer an explicit map entry; fall back to the
	// universal default for models not in the map.
	override, ok := modelOverrideMap[result.OriginalModel]
	if !ok {
		override = defaultFlashModel
	}

	// No-op if the resolved override equals the current model.
	if override == result.OriginalModel {
		return result
	}

	result.OverrideModel = override
	result.OverrideApplied = true
	result.OverrideReason = string(result.RequestClass) + " downgrade (" + result.OriginalModel + " → " + override + ")"
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

// classifyRequest inspects the last user message and request structure to
// infer the task type. Returns ClassUnknown when no clear signal is found.
//
// Classification priority (higher matches first):
//  1. Structured extraction (response_format json_object / json_schema)
//  2. Automation / workflows (git, PR, branch — mechanical orchestration)
//  3. Complex reasoning (long + multi-requirement)
//  4. Editing / refactoring (modify, edit, write)
//  5. Code search (find, search, explore)
//  6. Simple lookup (question-like, explanation)
//  7. Catch-all: short message → simple lookup; everything else → unknown
//
// Lint tasks (fix + lint/remove unused/rename) are a sub-case of automation:
// when the message contains both editing keywords AND lint keywords, the
// lint signal wins (mechanical, flash handles it). When the message contains
// strong editing keywords (refactor, rewrite, implement) alongside
// automation keywords, editing wins (creative work needs pro).
//
// Classification operates on the stripped message (Cursor XML/summary
// blocks removed) so we classify on the actual prompt, not the wrapper.
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

	// Structured extraction: response_format json_object / json_schema.
	// This is independent of message content and takes highest priority.
	if hasStructuredOutput(body) {
		return ClassStructuredExtraction
	}

	// Automation / workflow: git commands, PR creation, branch operations,
	// scripts — deterministic tool-orchestration tasks that flash handles well.
	// Checked before editing so "create a PR" routes to automation.
	// When both automation AND strong-editing keywords are present, editing wins:
	// "refactor and create a PR" needs pro for the refactoring part.
	// Exception: lint/linter tasks are mechanical (remove unused code, rename) —
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

	// Editing / refactoring: message mentions editing, modifying, refactoring.
	// isEditing fires after automation and complex reasoning so purely
	// mechanical or planning tasks don't get caught here.
	if isEditing(lower) {
		return ClassEditing
	}

	// Code search / exploration: message mentions searching, finding, exploring.
	// Checked before simple lookup because short search messages exist.
	if isCodeSearch(lower) {
		return ClassCodeSearch
	}

	// Simple lookup: question-like message, explanation request, or short
	// ambiguous prompt with no stronger signal. This is the catch-all for
	// cheap-to-route queries.
	if isSimpleLookup(lower, stripped) {
		return ClassSimpleLookup
	}

	// Catch-all: short messages (≤120 chars) that didn't match any specific
	// intent class are treated as simple lookups.
	if len(stripped) > 0 && len(stripped) <= 120 {
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
// or locate code without modifying it.
func isCodeSearch(lower string) bool {
	searchKeywords := []string{
		"find", "search", "locate", "look for", "look up",
		"grep", "explore", "discover", "what files",
		"where is the", "where are the", "show me the",
		"list", "scan", "inspect",
		"check the", "check if", "check how",
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
	// suggest complex multi-part reasoning.
	if len(stripped) >= 400 {
		sentences := strings.Count(stripped, ".") + strings.Count(stripped, "?") + strings.Count(stripped, "!")
		if sentences >= 3 {
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

// messageRoles returns a compact role-sequence string for logging, e.g.
// "system, user, tool, user".
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
