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
)
