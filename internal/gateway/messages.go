package gateway

import (
	"strings"
)

const syntheticToolResult = "Tool call did not return a result."
const recoveredCallName = "recovered_tool_call"

func repairToolCallPairingInPlace(messages *[]any) {
	original := append([]any(nil), (*messages)...)
	var result []any
	usedIDs := map[string]bool{}
	var open []string
	renames := map[string][]string{}
	blockIdx := -1
	idCounter := 0

	closeOpen := func() {
		for _, id := range open {
			result = append(result, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      syntheticToolResult,
			})
		}
		open = open[:0]
	}

	nextRecoveredID := func() string {
		for {
			idCounter++
			id := "call_recovered_" + itoa(idCounter)
			if !usedIDs[id] {
				usedIDs[id] = true
				return id
			}
		}
	}

	for _, raw := range original {
		msg, ok := raw.(map[string]any)
		if !ok {
			result = append(result, raw)
			continue
		}
		role := stringField(msg, "role")
		calls, hasCalls := arrayField(msg, "tool_calls")

		if role == "assistant" && hasCalls && len(calls) > 0 {
			closeOpen()
			renames = map[string][]string{}
			for i, c := range calls {
				call, ok := c.(map[string]any)
				if !ok {
					continue
				}
				rawID := stringField(call, "id")
				var finalID string
				if rawID != "" && !usedIDs[rawID] {
					usedIDs[rawID] = true
					finalID = rawID
				} else {
					newID := nextRecoveredID()
					renames[rawID] = append(renames[rawID], newID)
					call["id"] = newID
					finalID = newID
					calls[i] = call
				}
				open = append(open, finalID)
			}
			msg["tool_calls"] = calls
			result = append(result, msg)
			blockIdx = len(result) - 1
		} else if role == "tool" {
			rawID := stringField(msg, "tool_call_id")
			mappedID := rawID
			if queue, ok := renames[rawID]; ok && len(queue) > 0 {
				mappedID = queue[0]
				renames[rawID] = queue[1:]
			}
			pos := -1
			for i, id := range open {
				if id == mappedID {
					pos = i
					break
				}
			}
			if pos >= 0 {
				open = append(open[:pos], open[pos+1:]...)
				msg["tool_call_id"] = mappedID
				result = append(result, msg)
			} else {
				finalID := mappedID
				if finalID == "" || usedIDs[finalID] {
					finalID = nextRecoveredID()
				} else {
					usedIDs[finalID] = true
				}
				syntheticCall := map[string]any{
					"id":   finalID,
					"type": "function",
					"function": map[string]any{
						"name":      recoveredCallName,
						"arguments": "{}",
					},
				}
				if blockIdx >= 0 && blockIdx < len(result) {
					if assistant, ok := result[blockIdx].(map[string]any); ok {
						if tc, ok := arrayField(assistant, "tool_calls"); ok {
							tc = append(tc, syntheticCall)
							assistant["tool_calls"] = tc
							result[blockIdx] = assistant
						}
					}
				} else {
					result = append(result, map[string]any{
						"role":       "assistant",
						"content":    nil,
						"tool_calls": []any{syntheticCall},
					})
					blockIdx = len(result) - 1
				}
				msg["tool_call_id"] = finalID
				result = append(result, msg)
			}
		} else {
			closeOpen()
			renames = map[string][]string{}
			blockIdx = -1
			result = append(result, msg)
		}
	}
	closeOpen()
	*messages = result
}

func sanitizeMessage(msg map[string]any, injectReasoning bool, registry map[string]string, stripImages bool) {
	if stringField(msg, "role") == "developer" {
		msg["role"] = "system"
	}
	delete(msg, "name")
	delete(msg, "cache_control")

	calls, hasToolCalls := arrayField(msg, "tool_calls")
	if hasToolCalls && len(calls) > 0 {
		if injectReasoning {
			rc := stringField(msg, "reasoning_content")
			if strings.TrimSpace(rc) == "" {
				msg["reasoning_content"] = reasoningPlaceholder
			}
		}
		if content, ok := msg["content"]; !ok || content == nil {
			msg["content"] = nil
		}
		for i, c := range calls {
			if call, ok := c.(map[string]any); ok {
				sanitizeToolCall(call, registry)
				calls[i] = call
			}
		}
		msg["tool_calls"] = calls
	} else if stringField(msg, "role") == "assistant" {
		delete(msg, "reasoning_content")
	}

	if stringField(msg, "role") == "tool" {
		if nameVal, ok := msg["name"]; ok {
			n := nameVal
			remapNameField(&n, registry)
			msg["name"] = n
		}
		if content, ok := msg["content"]; ok {
			if _, isObj := content.(map[string]any); isObj {
				msg["content"] = toJSONString(content)
			} else if _, isArr := content.([]any); isArr {
				msg["content"] = toJSONString(content)
			}
		}
		if content, ok := msg["content"]; !ok || content == nil {
			msg["content"] = reasoningPlaceholder
		}
	}

	if content, ok := msg["content"]; ok {
		sanitizeMessageContent(&content, stripImages)
		msg["content"] = content
	}
}

func sanitizeToolCall(call map[string]any, registry map[string]string) {
	if stringField(call, "type") == "" {
		call["type"] = "function"
	}
	if fn, ok := mapField(call, "function"); ok {
		if nameVal, ok := fn["name"]; ok {
			n := nameVal
			remapNameField(&n, registry)
			fn["name"] = n
		}
		if args, ok := fn["arguments"]; ok {
			if _, isStr := args.(string); !isStr {
				fn["arguments"] = toJSONString(args)
			}
		}
	}
}

func sanitizeMessageContent(content *any, stripImages bool) {
	parts, ok := (*content).([]any)
	if !ok {
		if s, ok := (*content).(string); ok && s == "" {
			*content = reasoningPlaceholder
		}
		return
	}
	var kept []any
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		delete(part, "cache_control")
		partType := stringField(part, "type")
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "text":
			kept = append(kept, cloneMap(part))
		case "image_url", "input_image", "image_file":
			if stripImages {
				kept = append(kept, map[string]any{"type": "text", "text": imageOmittedPlaceholder})
			} else {
				kept = append(kept, cloneMap(part))
			}
		default:
			if text := stringField(part, "text"); text != "" {
				kept = append(kept, map[string]any{"type": "text", "text": text})
			}
		}
	}
	if len(kept) == 0 {
		*content = reasoningPlaceholder
	} else if len(kept) == 1 {
		if m, ok := kept[0].(map[string]any); ok {
			if text := stringField(m, "text"); text != "" {
				*content = text
				return
			}
		}
		*content = kept[0]
	} else {
		*content = kept
	}
}

// messagesContainPlaceholder returns true when any message in the slice contains
// the imageOmittedPlaceholder text — used after sanitization to decide whether
// to inject the image-stripped system note for DeepSeek.
func messagesContainPlaceholder(msgs []any) bool {
	for _, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if valueContainsPlaceholder(msg["content"]) {
			return true
		}
	}
	return false
}

func valueContainsPlaceholder(v any) bool {
	switch x := v.(type) {
	case string:
		return x == imageOmittedPlaceholder
	case []any:
		for _, part := range x {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if stringField(m, "text") == imageOmittedPlaceholder {
				return true
			}
		}
	}
	return false
}

// prependSystemNote inserts a system message at the front of the message slice.
// If the first message is already a system message, it merges the note into it.
func prependSystemNote(msgs []any, note string) []any {
	if len(msgs) > 0 {
		if first, ok := msgs[0].(map[string]any); ok && stringField(first, "role") == "system" {
			existing := stringField(first, "content")
			if existing != "" {
				first["content"] = note + "\n" + existing
			} else {
				first["content"] = note
			}
			msgs[0] = first
			return msgs
		}
	}
	return append([]any{map[string]any{"role": "system", "content": note}}, msgs...)
}
