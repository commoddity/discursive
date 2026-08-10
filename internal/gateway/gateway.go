// Package gateway sanitizes Cursor OpenAI-compat requests and adapts Responses
// API payloads to Chat Completions before upstream proxy (T05).
//
// Contract: may depend on internal/config for Provider; must not log secrets.
package gateway

const (
	reasoningPlaceholder = " "
	maxFunctionNameLen   = 64
	minFunctionNameLen   = 3
	maxSchemaDepth       = 9

	defaultMaxTokens = 32768
	maxTokensCap     = 512000

	probeUserContent = "Hi"

	// imageOmittedPlaceholder replaces vision parts — DeepSeek/Moonshot chat
	// rejects image_url; Cursor still resends screenshots in Agent history.
	imageOmittedPlaceholder = "[image omitted]"

	// imageStrippedWarning is prepended as a system-level note when DeepSeek
	// strips image content from the request so the user knows images were lost.
	imageStrippedWarning = "[note: one or more images in this conversation were removed because the selected model does not support vision. Switch to a Kimi model for image-aware responses.]"

	// flashVerbosityMaxTokens is the output-token ceiling applied to
	// deepseek-v4-flash when verbosity control is enabled. It is generous
	// enough for substantive coding responses but prevents the model from
	// emitting run-away prose.
	flashVerbosityMaxTokens = 4096
)

// flashTersenessDirective is appended to deepseek-v4-flash system messages
// when verbosity control is enabled. It uses an authority-marked, numbered
// constraint format for maximum LLM adherence (DeepSeek models follow
// enumerated, verifiable constraints better than descriptive prose).
const flashTersenessDirective = "" +
	"CRITICAL OUTPUT CONSTRAINT — ALWAYS FOLLOW:\n" +
	"\n" +
	"1. Code first, prose second. Lead with the solution, not the explanation.\n" +
	"2. After delivering code, summarize in at most 1 short sentence.\n" +
	"   Do NOT restate what the code does line-by-line.\n" +
	"3. Never write more than 2 sentences of prose without code between them.\n" +
	"4. If a single-line code change solves the problem, return ONLY that line.\n" +
	"5. Omit: pleasantries, recaps, \"let me explain\", \"here's why\",\n" +
	"   \"I hope this helps\", and all other conversational filler."
