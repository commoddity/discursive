package gateway

import (
	"encoding/json"
	"hash/fnv"
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
	// DiagnosticDump controls whether the full messages array is written to
	// /tmp/discursive-msgdump-<requestID>.json when content extraction fails
	// (stripped_len == 0). Off by default and gated behind a separate CLI flag
	// so it never fires accidentally in production. Files in /tmp/ are
	// automatically cleaned on reboot (macOS) or by tmpfiles.d (Linux).
	DiagnosticDump bool
}

// TurnType classifies what kind of agent-loop turn a request represents,
// based on the role of the last message in the messages array.
type TurnType string

const (
	// TurnToolResult means the last message role is "tool" — the model just
	// received a tool result and must decide the next step. These are the
	// candidate turns for downgrade to a cheaper model.
	TurnToolResult TurnType = "tool_result"
	// TurnUserPrompt means the last message role is "user" or "developer" —
	// a fresh human prompt. The content classifier (classifyRequest) handles
	// these.
	TurnUserPrompt TurnType = "user_prompt"
	// TurnAgentContinue means the last message role is "assistant" —
	// continuation or prefill scenario. Conservative: keep model.
	TurnAgentContinue TurnType = "agent_continue"
	// TurnUnknown means empty messages, system-only, or unparseable
	// structure. Conservative: keep model.
	TurnUnknown TurnType = "unknown"
)

// OverrideTier states how aggressively a request should be downgraded.
// It replaces the old boolean "should downgrade" with a 3-way decision so the
// router can route cheap turns to flash, decision-heavy cheap turns to pro,
// and everything else to the original (kept) model.
type OverrideTier string

const (
	// TierKeep means keep the original model — no override.
	TierKeep OverrideTier = "keep"
	// TierPro means downgrade to a mid-tier model that still reasons well
	// (e.g. deepseek-v4-pro) — used for decision-heavy cheap tool results.
	TierPro OverrideTier = "pro"
	// TierFlash means downgrade to the cheapest model (e.g. deepseek-v4-flash)
	// — used for read-only / low-risk tool results and simple content classes.
	TierFlash OverrideTier = "flash"
)

// toolVerbosityCap is the max completion tokens we inject into cheap
// (flash) tool-result turns. It exists so cheap turns cannot balloon into
// multi-thousand-token streams (4–16k tokens / 20–106s latencies were observed
// in real runs), which is the real cause of "the flow took too long". Keeping
// cheap turns terse makes the whole agent loop faster at negligible cost.
const toolVerbosityCap = 1500

// Read-only tools never change state; their results only need cheap
// interpretation. Write / decision tools (StrReplace, Shell, notebooks, etc.)
// may warrant a mid-tier (pro) model when their results are non-trivial.
var readOnlyToolNames = map[string]bool{
	"Read":             true,
	"Grep":             true,
	"Glob":             true,
	"WebSearch":        true,
	"WebFetch":         true,
	"FetchMcpResource": true,
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
	TurnType          TurnType     `json:"turn_type"`
	ToolResultSize    string       `json:"tool_result_size,omitempty"`
	ToolName          string       `json:"tool_name,omitempty"`
	OverrideTier      OverrideTier `json:"override_tier,omitempty"`
	MaxTokens         int          `json:"max_tokens,omitempty"`
}

// modelOverrideMap maps expensive models to cheaper equivalents for flash-tier
// downgrades. Entries take priority; models not in the map fall back to
// defaultFlashModel.
var modelOverrideMap = map[string]string{
	"deepseek-v4-pro": "deepseek-v4-flash",
	// "glm-5.2":         "glm-4.7", // Uncomment to use glm-4.7 as flash for glm-5.2
}

// proModelOverrideMap maps expensive models to a mid-tier pro equivalent for
// decision-heavy downgrades (medium write/error tool results). Entries take
// priority; models not in the map fall back to defaultProModel. Models that
// are already pro-or-below (e.g. deepseek-v4-pro itself) have no entry, so a
// pro-tier override is a no-op for them.
var proModelOverrideMap = map[string]string{
	"glm-5.2":         "deepseek-v4-pro",
	"kimi-k3":         "deepseek-v4-pro",
	"kimi-k2.7-code":  "deepseek-v4-pro",
	"deepseek-v4":     "deepseek-v4-pro",
	"deepseek-v4-pro": "deepseek-v4-pro",
}

// defaultFlashModel is the fallback model for any traffic that triggers a
// cheap downgrade but whose original model isn't in modelOverrideMap. Anything
// not in the map (including models that aren't DeepSeek at all) falls back to
// this.
const defaultFlashModel = "deepseek-v4-flash"

// defaultProModel is the fallback model for pro-tier downgrades of originals
// not present in proModelOverrideMap.
const defaultProModel = "deepseek-v4-pro"

// overrideModelForTier resolves the model to send for a downgrade of the given
// tier. flash uses modelOverrideMap (fallback defaultFlashModel); pro uses
// proModelOverrideMap (fallback defaultProModel). keep returns the original
// model unchanged.
func overrideModelForTier(original string, tier OverrideTier) string {
	switch tier {
	case TierPro:
		if m, ok := proModelOverrideMap[original]; ok {
			return m
		}
		return defaultProModel
	case TierFlash:
		if m, ok := modelOverrideMap[original]; ok {
			return m
		}
		return defaultFlashModel
	default:
		return original
	}
}

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

	// Step 2: turn type detection for per-turn routing instrumentation.
	result.TurnType = detectTurnType(body)
	result.ToolResultSize = toolResultSize(body)
	result.ToolName = toolResultName(body)

	// Step 3: content-based classification.
	result.RequestClass = classifyRequest(body)

	// Determine this turn's routing tier. Two independent signals contribute:
	//   (a) content class — simple lookup, code search, structured extraction, ...
	//   (b) per-turn — tool-result rounds within a multi-step agent flow.
	// The per-turn signal is decision-aware: cheap (small / medium read-only)
	// results route to flash, non-trivial write/error results route to pro,
	// and large results keep the original model (big outputs may need
	// pro-level interpretation).
	tier := overrideTier(result, body)
	result.OverrideTier = tier

	// Verbosity cap: cheap (flash) tiers get a tight max_tokens so they cannot
	// balloon into multi-thousand-token streams that stall the agent loop.
	if tier == TierFlash {
		result.MaxTokens = toolVerbosityCap
	}

	// Always log classification at DEBUG so operators can tune thresholds.
	lastMsg := lastUserMessage(body)
	lastMsgStripped := stripCursorNoise(lastMsg)
	slog.Debug("router: classify",
		"request_id", requestID,
		"request_class", result.RequestClass,
		"turn_type", result.TurnType,
		"tool_result_size", result.ToolResultSize,
		"tool_name", result.ToolName,
		"override_tier", result.OverrideTier,
		"max_tokens", result.MaxTokens,
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
	// so we can analyze Cursor's message structure and refine our extraction,
	// and — during debugging sessions (--diagnostic-dump is always operator-gated
	// and off by default) — when a classify event is otherwise not visible from
	// the log lines alone. These are:
	//   * stripped_len == 0            → extraction failed (original reason)
	//   * tool-result turns            → validate size bucketing + tool schema
	//   * overrides actually applied   → confirm the downgrade (model+provider)
	//   * no-op extraction             → stripped_len == last_msg_len warns that
	//                                    stripCursorNoise stripped nothing, so the
	//                                    classifier may be missing structure
	//   * 1% random sample             → catch unforeseen shapes current
	//                                    heuristics can't predict
	// Dump happens before routing so the raw message array is captured verbatim.
	if r.cfg.DiagnosticDump && shouldDumpForDiagnostics(result.TurnType, result.ToolResultSize, tier != TierKeep, len(lastMsg), len(lastMsgStripped), requestID) {
		dumpMessages(body, requestID)
	}

	if !r.cfg.Enabled {
		return result
	}

	// No override when the tier says to keep the original model.
	if tier == TierKeep {
		return result
	}

	// Apply the verbosity cap first so it also applies when the effective tier
	// is flash but the model override below is a no-op (e.g. the flow already
	// runs on deepseek-v4-flash). Flash-tier turns are always kept terse to
	// avoid multi-thousand-token streams that stall the agent loop. Applied
	// before the model override so a no-op override still gets the cap.
	if tier == TierFlash {
		body["max_tokens"] = toolVerbosityCap
	}

	// Resolve the override model for the chosen tier. Both maps fall back to a
	// provider-agnostic default so models not explicitly mapped still downgrade.
	override := overrideModelForTier(result.OriginalModel, tier)

	// No-op if the resolved override equals the current model (e.g. a pro-tier
	// request that is already on deepseek-v4-pro).
	if override == result.OriginalModel {
		return result
	}

	result.OverrideModel = override
	result.OverrideApplied = true

	// Build a reason that identifies which signal triggered the downgrade.
	result.OverrideReason = overrideReason(result, tier) + " downgrade (" + result.OriginalModel + " → " + override + ")"

	// Apply the model override to the body (proxy re-resolves the provider).
	body["model"] = override

	slog.Info("router: model_overridden",
		"request_id", requestID,
		"request_class", result.RequestClass,
		"turn_type", result.TurnType,
		"tool_result_size", result.ToolResultSize,
		"tool_name", result.ToolName,
		"override_tier", result.OverrideTier,
		"max_tokens", result.MaxTokens,
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

// overrideTier decides the routing tier for a request. It combines the content
// class (a) with the per-turn tool-result decision (b):
//
//	Content-driven downgrade triggered → TierFlash (cheap is fine for simple
//	lookup / code search / structured extraction / automation).
//
//	Tool-result turn (last role == tool):
//	    small + read-only tool     → TierFlash (short grep/read/glob output)
//	    small + write/error tool   → TierPro   (deciding the next step after an
//	                                            edit/shell/test needs pro reasoning;
//	                                            flash here causes loop flailing)
//	    medium + read-only tool    → TierFlash (just reading: grep/read/search)
//	    medium + write/error tool  → TierPro   (decision-heavy: shell, edits,
//	                                            tests, failures — needs pro)
//	    large                      → TierKeep  (big outputs need pro reasoning)
//	    unknown / empty size       → TierKeep  (can't gauge risk → conservative)
//
//	Everything else → content-class result (shouldDowngrade) or TierKeep.
func overrideTier(result ClassifierResult, body map[string]any) OverrideTier {
	// Per-turn tool-result signal first — it is the primary driver of cheap
	// turns within a multi-step agent flow.
	if result.TurnType == TurnToolResult {
		write := isWriteTool(result.ToolName)
		switch result.ToolResultSize {
		case "small":
			if write {
				return TierPro
			}
			return TierFlash
		case "medium":
			if write {
				return TierPro
			}
			return TierFlash
		case "large":
			return TierKeep
		default:
			// Unknown/empty size: we can't gauge risk, so keep the original
			// model (conservative).
			return TierKeep
		}
	}

	// Non-tool turns follow the content classifier.
	if shouldDowngrade(result.RequestClass) {
		return TierFlash
	}
	return TierKeep
}

// overrideReason builds a human-readable reason for the override log, naming
// whichever signal(s) triggered the tier.
func overrideReason(result ClassifierResult, tier OverrideTier) string {
	var parts []string
	if result.TurnType == TurnToolResult && result.ToolResultSize != "" {
		parts = append(parts, string(result.TurnType)+"/"+result.ToolResultSize)
		if result.ToolName != "" {
			parts = append(parts, "tool="+result.ToolName)
		}
	} else if result.RequestClass != "" && result.RequestClass != ClassUnknown {
		parts = append(parts, string(result.RequestClass))
	}
	if tier == TierPro {
		parts = append(parts, "decision-heavy→pro")
	}
	if len(parts) == 0 {
		return string(tier)
	}
	return strings.Join(parts, "+")
}

// isWriteTool returns true when a tool name represents a state-changing or
// decision-heavy operation whose (non-small) result warrants a pro model.
// Read-only tools (Read/Grep/Glob/search) → false. Everything unknown is
// treated as a write/decision tool so we default to the more conservative
// (pro) tier for medium results.
func isWriteTool(name string) bool {
	if name == "" {
		return true
	}
	return !readOnlyToolNames[name]
}

// classifyRequest inspects the last user message and request structure to
// infer the task type. Returns ClassUnknown when no clear signal is found.
//
// Order matters: structured extraction is checked first (independent of message
// content), then automation, then complex reasoning (planning/architecture/strategy
// before editing so "create a plan" isn't caught by the broad "create" editing
// keyword), then editing, then code search, and finally simple lookup as the
// catch-all for short, low-signal messages.
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
		"plan", "planning",
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

// detectTurnType classifies the request by the role of its last message.
// Tool-result rounds (last role == "tool") are the primary targets for
// per-turn model downgrade within a multi-step agent flow.
func detectTurnType(body map[string]any) TurnType {
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return TurnUnknown
	}
	last, ok := msgs[len(msgs)-1].(map[string]any)
	if !ok {
		return TurnUnknown
	}
	role, _ := last["role"].(string)
	switch role {
	case "tool":
		return TurnToolResult
	case "user", "developer":
		return TurnUserPrompt
	case "assistant":
		return TurnAgentContinue
	default:
		return TurnUnknown
	}
}

// toolResultSize buckets the content size of the last tool-result message.
// Zero means the messages array is empty or the last message is not a tool.
// Buckets help identify where token spend concentrates:
//
//	<=512 chars  → "small"  (quick shell command outputs)
//	<=4096 chars → "medium" (diff outputs, file reads)
//	>4096 chars  → "large"  (full logs, stack traces)
func toolResultSize(body map[string]any) string {
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return ""
	}
	last, ok := msgs[len(msgs)-1].(map[string]any)
	if !ok {
		return ""
	}
	role, _ := last["role"].(string)
	if role != "tool" {
		return ""
	}
	content, _ := last["content"].(string)
	n := len(content)
	switch {
	case n <= 512:
		return "small"
	case n <= 4096:
		return "medium"
	default:
		return "large"
	}
}

// toolResultName returns the tool's function name for the last tool-result
// message, or "" when it can't be determined. It works by matching the last
// tool message's tool_call_id against the preceding assistant message's
// tool_calls[].id (or function call), returning the associated function name.
// This lets the router distinguish read-only tools (Read/Grep/Glob/search)
// from write/decision tools (Shell/StrReplace/notebook) to thread the
// decision-aware pro tier.
func toolResultName(body map[string]any) string {
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return ""
	}

	// Find the last tool message and its tool_call_id.
	var callID string
	lastIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}
		lastIdx = i
		callID, _ = msg["tool_call_id"].(string)
		break
	}
	if lastIdx < 0 {
		return ""
	}

	// Walk backward from the tool message for the assistant message that
	// issued the tool call with the matching id.
	for i := lastIdx - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}
		// Legacy shape: single function_call object.
		if fc, ok := msg["function_call"].(map[string]any); ok {
			name, _ := fc["name"].(string)
			if name != "" {
				return name
			}
		}
		// Standard shape: tool_calls array.
		tcs, ok := msg["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, tc := range tcs {
			entry, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := entry["id"].(string); id != "" && id != callID {
				continue
			}
			if fn, ok := entry["function"].(map[string]any); ok {
				if name, _ := fn["name"].(string); name != "" {
					return name
				}
			}
		}
	}
	return ""
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
// file for diagnostic analysis. Only called when the diagnostic-dump flag is on
// and the last user message is entirely Cursor boilerplate (stripped_len == 0).
//
// Files are written to /tmp/ as discursive-msgdump-<requestID>.json so they are
// automatically cleaned on reboot (macOS) or by tmpfiles.d (Linux).
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
	filename := filepath.Join("/tmp", "discursive-msgdump-"+requestID+".json")
	if err := os.WriteFile(filename, b, 0644); err != nil {
		slog.Warn("router: dump_messages write failed", "request_id", requestID, "err", err)
		return
	}
	slog.Info("router: dump_messages written", "request_id", requestID, "file", filename, "size_bytes", len(b))
}

// dumpSampleRate is the denominator for the deterministic sample of classify
// events we dump during a debugging session (1 in 100). It exists so a
// long-running --diagnostic-dump session surfaces shapes the targeted triggers
// can't predict, without writing a file per request.
const dumpSampleRate uint32 = 100

// shouldDumpForDiagnostics decides whether a classify event warrants a raw
// message dump during a debugging session. Dumping is always operator-gated by
// the --diagnostic-dump flag; this function only narrows which requests within
// that session produce a file so we don't write one per request even when the
// flag is on. It returns true when the event is otherwise invisible from the
// log lines alone:
//   - stripped_len == 0            extraction failed (always, original trigger)
//   - tool-result turn             validate size bucketing + tool content schema
//   - would_downgrade              confirm the model+provider downgrade
//   - no-op extraction             stripCursorNoise removed nothing — the
//     classifier may be missing structure
//   - 1% deterministic sample      catch unforeseen shapes heuristics miss
func shouldDumpForDiagnostics(turnType TurnType, toolResultSize string, wouldDowngrade bool, lastMsgLen, strippedLen int, requestID string) bool {
	// Extraction failed — original diagnostic trigger, always dump.
	if strippedLen == 0 {
		return true
	}
	// Tool-result turns: validate size bucketing and tool content schema.
	if turnType == TurnToolResult {
		return true
	}
	// Overrides actually applied: confirm the downgrade sent the expected model
	// and provider, and nothing was lost in provider re-resolution.
	if wouldDowngrade {
		return true
	}
	// No-op extraction: if stripping removed nothing, the classifier may be
	// missing structure it should have seen.
	if lastMsgLen > 0 && strippedLen == lastMsgLen {
		return true
	}
	// Deterministic sample so long-running sessions surface unforeseen shapes.
	// Hash the requestID (a random per-request string) and gate on a modulus;
	// hashing avoids mutable per-process state and is reproducible.
	h := fnv.New32a()
	_, _ = h.Write([]byte(requestID))
	return h.Sum32()%dumpSampleRate == 0
}
