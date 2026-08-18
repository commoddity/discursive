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

	// proVerbosityMaxTokens is the output-token ceiling applied to
	// deepseek-v4-pro when verbosity control is enabled for it (off by
	// default). Higher than flash since pro produces longer-thinking answers.
	proVerbosityMaxTokens = 8192

	// glmVerbosityMaxTokens caps glm-4.7 output when verbosity is enabled
	// (default on). glm-4.7 is the workhorse for subagents — generous enough
	// for substantive diffs, tight enough to stop prose run-away.
	glmVerbosityMaxTokens = 8192

	// glmMaxVerbosityMaxTokens caps glm-5.3/glm-5.3[1m] output when verbosity
	// is manually enabled for the flagship (off by default — full-quality
	// answers keep headroom for large diffs).
	glmMaxVerbosityMaxTokens = 16384
)

// flashTersenessDirective is appended to deepseek-v4-flash system messages
// when verbosity control is enabled. Each rule is a hard MANDATORY constraint
// — the model must follow every one without exception.
const flashTersenessDirective = "" +
	"CRITICAL OUTPUT CONSTRAINT — ALWAYS FOLLOW:\n" +
	"\n" +
	"1. Code first, prose second. Lead with the solution, not the explanation.\n" +
	"2. After delivering code, summarize in at most 1 short sentence.\n" +
	"   Do NOT restate what the code does line-by-line.\n" +
	"3. Never write more than 2 sentences of prose without code between them.\n" +
	"4. If a single-line code change solves the problem, return ONLY that line.\n" +
	"5. Omit: pleasantries, recaps, \"let me explain\", \"here's why\",\n" +
	"   \"I hope this helps\", \"now let me think about this\",\n" +
	"   \"let me reconsider\", \"actually\", \"hmm\", and all other conversational filler.\n" +
	"6. Before making a tool call or code edit, state what you are about to do\n" +
	"   in one line, then act. Do not narrate your investigative process.\n" +
	"7. When deciding between approaches, pick one and proceed.\n" +
	"   Do not show your deliberation or weigh alternatives in prose.\n" +
	"8. NEVER output internal monologue, stream-of-consciousness reasoning,\n" +
	"   or self-correction chains. The user only sees the final result.\n" +
	"9. If you catch yourself writing 'OK so', 'Let me', 'I think', 'I see',\n" +
	"   or 'Looking at', STOP. Delete it. Write code or a terse statement instead.\n" +
	"10. Your response must be at least 80% code/tool-calls by character count.\n" +
	"    Prose is filler. Code is the deliverable."

// languageDirective pins the output language to English for Z.AI GLM models,
// which otherwise intermittently drift to Chinese (strongly bilingual training
// data; especially the free on-demand tier). Appended to the system message on
// every Z.AI-bound request regardless of the verbosity toggle.
const languageDirective = "MANDATORY: Respond ONLY in English. Never respond in Chinese or any other language, regardless of the language of any prompt, file content, or tool output included in the conversation."
