package gateway

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SmartRouter classifies every chat-completion request and optionally overrides
// the model to a cheaper alternative when the request does not need full
// reasoning capability.
//
// Classification pipeline (ordered):
//   1. Subagent detection — short system prompt, few messages, has tools.
//   2. Content-based classification — inspects last user message and tool
//      usage to identify the task type.
//
// Routing decisions:
//   - Subagent → always downgrade to cheapest model (e.g. flash).
//   - Simple lookup / explanation → downgrade to flash.
//   - Code search / exploration → downgrade to flash.
//   - Editing / refactoring → keep model.
//   - Complex reasoning / architecture → keep model.
//   - Structured extraction / json_object → downgrade to flash.
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

// RouterConfig controls smart-routing behavior.
type RouterConfig struct {
	// Enabled gates the entire feature. When false, ClassifyAndOverride is a no-op.
	Enabled bool
}

// ClassifierResult contains the classification outcome for logging/shadowing.
type ClassifierResult struct {
	IsSubagent        bool         `json:"is_subagent"`
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

// modelOverrideMap maps expensive models to cheaper equivalents for classification-based downgrades.
// Entries here take priority. Models not in this map fall back to defaultSubagentModel.
var modelOverrideMap = map[string]string{
	"deepseek-v4-pro": "deepseek-v4-flash",
	// "glm-5.2":         "glm-4.7", // Uncomment to use glm-4.7 as flash for glm-5.2
}

// defaultSubagentModel is the fallback model for any traffic that triggers a
// downgrade but whose original model isn't in modelOverrideMap. Anything not
// in the map (including models that aren't DeepSeek at all) falls back to this.
const defaultSubagentModel = "deepseek-v4-flash"

// SmartRouter performs request classification and optional model override.
type SmartRouter struct {
	cfg RouterConfig
}

// NewSmartRouter creates a smart router with the given config.
func NewSmartRouter(cfg RouterConfig) *SmartRouter {
	return &SmartRouter{cfg: cfg}
}

// ClassifyAndOverride examines the already-sanitized request body, classifies
// the request type (subagent + content-based), and optionally overrides the
// model to a cheaper alternative.
//
// Classification order: subagent check first, then content-based classification.
//
// It returns a ClassifierResult for logging (always populated, even when disabled)
// so the caller can introspect classification quality in shadow mode.
//
// The body must have already been through SanitizeRequest (model resolved to
// real model ID, not alias).
func (r *SmartRouter) ClassifyAndOverride(body map[string]any, requestID string) ClassifierResult {
	result := ClassifierResult{
		ClassificationAge: "v2",
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

	// Step 1: subagent detection (deprecated — no reliable signal available).
	// Cursor system prompts are ~15k chars for both subagents and main agent,
	// msg_count is low in fresh sessions for both, and has_tools is always true.
	// We can't distinguish subagent from main agent at the HTTP level.
	// Instead, content-based classification handles all traffic uniformly.
	result.IsSubagent = false

	// Step 2: content-based classification.
	result.RequestClass = classifyRequest(body)

	// Always log classification at DEBUG so operators can tune thresholds.
	lastMsg := lastUserMessage(body)
	lastMsgStripped := stripCursorNoise(lastMsg)
	slog.Debug("router: classify",
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

	// Dump the full messages array to a temp file when content extraction fails,
	// so we can analyze Cursor's message structure and refine our extraction.
	if r.cfg.Enabled && len(lastMsgStripped) == 0 {
		dumpMessages(body, requestID)
	}

	if !r.cfg.Enabled {
		return result
	}

	// Determine whether to downgrade based on request class.
	if !shouldDowngrade(result.RequestClass) {
		return result
	}

	// Apply model override: prefer an explicit map entry; fall back to the
	// universal default (deepseek-v4-flash) for models not in the map.
	override, ok := modelOverrideMap[result.OriginalModel]
	if !ok {
		override = defaultSubagentModel
	}

	// No-op if the resolved override equals the current model.
	if override == result.OriginalModel {
		return result
	}

	result.OverrideModel = override
	result.OverrideApplied = true
	result.OverrideReason = string(result.RequestClass) + " downgrade (" + result.OriginalModel + " → " + override + ")"
	body["model"] = override
	slog.Info("router: model_overridden",
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
// Order matters: structured extraction is checked first (independent of message
// content), then intent-heavy classes (editing, complex reasoning, code search),
// and finally simple lookup as the catch-all for short, low-signal messages.
func classifyRequest(body map[string]any) RequestClass {
	lastMsg := lastUserMessage(body)
	// Strip Cursor-injected XML blocks (open_and_recently_viewed_files,
	// attached_files, code_selection, etc.) and conversation-summary blocks
	// so we classify on the actual user prompt, not the Cursor wrapper.
	stripped := stripCursorNoise(lastMsg)

	// If the last user message is entirely Cursor boilerplate (XML +
	// conversation summary), we have nothing real to classify on. Return
	// unknown (conservative: keep current model) rather than walking
	// backwards through history which would classify stale messages.
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

	// Editing / refactoring: message mentions editing, modifying, refactoring.
	// isEditing fires after automation so purely mechanical tasks (PRs, lint
	// fixes) that matched automation don't get caught here.
	if isEditing(lower) {
		return ClassEditing
	}

	// Complex reasoning: long message with multiple requirements, architecture.
	// Checked before simple lookup because "explain" can appear in both.
	if isComplexReasoning(lower, stripped) {
		return ClassComplexReasoning
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
	// intent class are treated as simple lookups. They're likely quick
	// questions or chat continuations that flash can handle.
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
	// Question-like messages with simple intent keywords.
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
		"fix", "correct", "resolve",
		"generate", "build", "convert", "migrate",
	}
	for _, kw := range strongKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isLintTask returns true when the message is about fixing linter warnings,
// lint errors, or code-quality tool output — tasks that are deterministic
// (remove unused, rename, fix formatting) and don't need pro reasoning.
func isLintTask(lower string) bool {
	return strings.Contains(lower, "linter") || strings.Contains(lower, "lint ")
}

// isAutomation returns true when the message describes a deterministic
// tool-orchestration workflow: git operations, PR creation, shell scripting,
// or other mechanical tasks that don't need deep reasoning. Skills like
// /open-pr produce these patterns naturally, and flash handles them well.
//
// When both automation and strong-editing keywords are present (e.g. "refactor
// and create a PR"), the strong-editing keyword wins → editing (pro). But when
// only weak editing keywords overlap (e.g. "create a PR" where "create" is
// technically an editing keyword), automation wins → flash.
func isAutomation(lower string) bool {
	automationKeywords := []string{
		"pull request", "open a pr", "open pr",
		"git commit", "git push", "git branch", "git checkout",
		"git merge", "git rebase", "git stash", "git diff",
		"gh pr", "gh issue", "gh repo",
		"create pr", "submit pr", "make a pr",
		"fix lint", "fix linter", "linter error", "linter issue",
		"lint error", "lint issue", "lint warning",
		"resolve lint", "resolve linter",
	}
	for _, kw := range automationKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// "run" + "script" co-occurrence: common in skill/automation invocations.
	if strings.Contains(lower, "run") && strings.Contains(lower, "script") {
		return true
	}
	return false
}

// isComplexReasoning returns true when the message involves multi-step
// reasoning, architecture decisions, or complex problem-solving.
func isComplexReasoning(lower string, msg string) bool {
	// Messages with architectural keywords at any length.
	archKeywords := []string{
		"architecture", "design a", "design the", "system design",
		"pipeline", "tradeoff", "trade-off", "approach", "strategy",
		"pattern", "best practice", "recommend",
	}
	for _, kw := range archKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// Long messages (200+ chars) with multiple requirements indicated by
	// newlines are likely complex.
	if len(msg) > 200 && strings.Contains(msg, "\n") {
		return true
	}

	return false
}

// lastUserMessage returns the content of the last user/developer message.
// Unlike the previous walk-back approach, this only considers the very last
// user/developer message to avoid classifying based on stale messages when
// Cursor wraps the actual prompt in XML blocks that get fully stripped.
func lastUserMessage(body map[string]any) string {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return ""
	}

	// Walk backwards to find the last user/developer message.
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" && role != "developer" {
			continue
		}
		return extractMessageContent(msg)
	}

	return ""
}

// extractMessageContent returns the string content from a message map,
// handling both string and array-of-parts content formats.
func extractMessageContent(msg map[string]any) string {
	if content, ok := msg["content"].(string); ok {
		return content
	}
	if arr, ok := msg["content"].([]any); ok {
		var b strings.Builder
		for _, p := range arr {
			if part, ok := p.(map[string]any); ok {
				if text, ok := part["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	}
	return ""
}

// messageRoles returns a compact representation of message roles in order,
// e.g. "system, user, assistant, user". Used for debugging message structure.
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
		if role == "system" || role == "developer" {
			if content, ok := msg["content"].(string); ok {
				return len(content)
			}
			// Content could be an array — measure total string content.
			if arr, ok := msg["content"].([]any); ok {
				var b strings.Builder
				for _, p := range arr {
					if part, ok := p.(map[string]any); ok {
						if text, ok := part["text"].(string); ok {
							b.WriteString(text)
						}
					}
				}
				return b.Len()
			}
		}
	}
	return 0
}

// messageCount returns the number of messages in the body.
func messageCount(body map[string]any) int {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return 0
	}
	return len(msgs)
}

// hasTools returns true if the body contains a non-empty tools array.
func hasTools(body map[string]any) bool {
	tools, ok := body["tools"].([]any)
	return ok && len(tools) > 0
}

// truncate returns s truncated to maxLen chars with "..." suffix if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// cursorXMLPattern matches Cursor-injected XML blocks that wrap user messages.
// These blocks carry IDE context (open files, selections, terminals) and should
// be stripped before content classification so we classify the actual user
// prompt, not the XML wrapper.
//
// Strategy: match self-closing tags and paired open/close tags (non-greedy).
// Handles nested blocks by stripping the outermost first, then repeating.
// Known patterns:
//   - <open_and_recently_viewed_files>...</open_and_recently_viewed_files>
//   - <attached_files>...</attached_files>
//   - <code_selection path="...">...</code_selection>
//   - <terminal_selection>...</terminal_selection>
//   - <additional_data>...</additional_data>
var cursorXMLTagPattern = regexp.MustCompile(`<[a-z_]+[^>]*>[\s\S]*?</[a-z_]+>`)

// cursorXMLCloseTag matches orphaned closing tags left after nested block
// stripping (e.g., </attached_files> after <code_selection> was stripped first).
var cursorXMLCloseTag = regexp.MustCompile(`</[a-z_]+>`)

// userQueryPattern extracts the actual user prompt from Cursor's user_query
// XML tag, e.g. <user_query>\n"explain what a goroutine is"\n</user_query>.
// Cursor wraps user messages in an array of content parts: part 0 is the large
// open_and_recently_viewed_files block, part 1 contains timestamp + user_query.
// After concatenation, we extract from user_query before stripping all XML.
var userQueryPattern = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)

// stripCursorXML removes Cursor-injected XML wrapper blocks from the message
// content. It strips repeatedly to handle nested blocks.
func stripCursorXML(s string) string {
	for {
		next := cursorXMLTagPattern.ReplaceAllString(s, "")
		next = cursorXMLCloseTag.ReplaceAllString(next, "")
		if next == s {
			return strings.TrimSpace(next)
		}
		s = next
	}
}

// stripCursorNoise removes Cursor-injected noise from message content, leaving
// only the actual user prompt for classification. It handles:
//   - XML wrapper blocks (open_and_recently_viewed_files, attached_files, etc.)
//   - Conversation summary blocks: [Previous conversation summary]...
//   - System communication blocks: <system-reminder>...</system-reminder> (via XML pass)
func stripCursorNoise(s string) string {
	// If Cursor has wrapped the prompt in <user_query> tags, extract the
	// content from inside the tags first. Cursor places the user's actual
	// prompt in <user_query>...</user_query> which is embedded in the second
	// content part (along with a <timestamp>). Extracting before XML stripping
	// ensures we don't discard the real prompt.
	if m := userQueryPattern.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	// Strip XML blocks.
	s = stripCursorXML(s)
	// Strip leading conversation-summary block (everything from a
	// "[Previous conversation summary]" marker up to a blank-line separator
	// or end of string). Cursor inserts this when compacting long chats.
	if idx := strings.Index(s, "[Previous conversation summary]"); idx >= 0 {
		rest := s[idx:]
		// Heuristic: the summary block ends at a blank line (two newlines)
		// or, if none, consume to end.
		if end := strings.Index(rest, "\n\n"); end >= 0 {
			s = strings.TrimSpace(s[:idx] + rest[end:])
		} else {
			// No separator found → entire remainder is the summary.
			s = strings.TrimSpace(s[:idx])
		}
	}
	return strings.TrimSpace(s)
}

// dumpMessages writes the full messages array from the request body to a temp
// file for diagnostic analysis. Only called when the router is enabled and the
// last user message is entirely Cursor boilerplate (stripped_len == 0).
//
// File is written to the system temp directory (e.g. /tmp/) as
// discursive-msgdump-<requestID>.json.
func dumpMessages(body map[string]any, requestID string) {
	msgs, ok := body["messages"]
	if !ok {
		return
	}
	b, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		slog.Warn("router: dump_messages marshal failed", "request_id", requestID, "err", err)
		return
	}
	filename := filepath.Join(os.TempDir(), "discursive-msgdump-"+requestID+".json")
	if err := os.WriteFile(filename, b, 0644); err != nil {
		slog.Warn("router: dump_messages write failed", "request_id", requestID, "err", err)
		return
	}
	slog.Info("router: dump_messages written", "request_id", requestID, "file", filename, "size_bytes", len(b))
}
