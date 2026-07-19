package gateway

import (
	"encoding/json"
	"strings"
)

// adaptCursorResponsesRequest converts Responses API input/instructions to messages.
func adaptCursorResponsesRequest(body map[string]any, stripImages bool) {
	if !messagesEmptyOrMissing(body) {
		delete(body, "input")
		delete(body, "instructions")
		stripResponsesOnlyFields(body)
		return
	}

	var instructions string
	if instr, ok := body["instructions"].(string); ok {
		instructions = instr
	}
	input, hasInput := body["input"]
	delete(body, "instructions")
	if hasInput {
		delete(body, "input")
		msgs := convertInputToMessages(input, instructions, stripImages)
		if len(msgs) > 0 {
			body["messages"] = msgs
		}
	}
	stripResponsesOnlyFields(body)
}

func messagesEmptyOrMissing(body map[string]any) bool {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return true
	}
	return len(msgs) == 0
}

func stripResponsesOnlyFields(body map[string]any) {
	for _, k := range []string{
		"previous_response_id", "conversation", "text",
		"prompt_cache_retention", "truncation", "include", "background",
	} {
		delete(body, k)
	}
}

func convertInputToMessages(input any, instructions string, stripImages bool) []any {
	var messages []any
	if strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": instructions,
		})
	}
	switch v := input.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			messages = append(messages, map[string]any{"role": "user", "content": v})
		}
	case []any:
		messages = append(messages, convertInputItems(v, stripImages)...)
	}
	return messages
}

func convertInputItems(items []any, stripImages bool) []any {
	var messages []any
	var pendingToolCalls []any
	var unansweredCallIDs []string
	synthIDCounter := 0

	handleOutput := func(item map[string]any) {
		flushPendingToolCalls(&messages, &pendingToolCalls, &unansweredCallIDs)
		fallback := ""
		if extractCallID(item) == "" && len(unansweredCallIDs) > 0 {
			fallback = unansweredCallIDs[0]
		}
		if msg := functionCallOutputToMessage(item, fallback); msg != nil {
			if id := stringField(msg, "tool_call_id"); id != "" {
				for i, uid := range unansweredCallIDs {
					if uid == id {
						unansweredCallIDs = append(unansweredCallIDs[:i], unansweredCallIDs[i+1:]...)
						break
					}
				}
			}
			messages = append(messages, msg)
		}
	}

	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		itemType := stringField(item, "type")
		switch itemType {
		case "function_call":
			synthIDCounter++
			pendingToolCalls = append(pendingToolCalls, functionCallToToolCall(item, &synthIDCounter))
		case "function_call_output":
			handleOutput(item)
		case "reasoning", "item_reference", "reasoning_item":
		case "input_image":
			flushPendingToolCalls(&messages, &pendingToolCalls, &unansweredCallIDs)
			if stripImages {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": imageOmittedPlaceholder,
				})
			}
		case "message", "":
			flushPendingToolCalls(&messages, &pendingToolCalls, &unansweredCallIDs)
			if msg := inputItemToMessage(item, stripImages); msg != nil {
				messages = append(messages, msg)
			}
		default:
			if strings.HasSuffix(itemType, "_call_output") {
				handleOutput(item)
			} else {
				flushPendingToolCalls(&messages, &pendingToolCalls, &unansweredCallIDs)
				if msg := inputItemToMessage(item, stripImages); msg != nil {
					messages = append(messages, msg)
				}
			}
		}
	}
	flushPendingToolCalls(&messages, &pendingToolCalls, &unansweredCallIDs)
	return messages
}

func flushPendingToolCalls(messages *[]any, pending *[]any, unanswered *[]string) {
	if len(*pending) == 0 {
		return
	}
	for _, call := range *pending {
		if m, ok := call.(map[string]any); ok {
			if id := stringField(m, "id"); id != "" {
				*unanswered = append(*unanswered, id)
			}
		}
	}
	*messages = append(*messages, map[string]any{
		"role":       "assistant",
		"content":    nil,
		"tool_calls": append([]any(nil), *pending...),
	})
	*pending = (*pending)[:0]
}

func inputItemToMessage(item map[string]any, stripImages bool) map[string]any {
	role := stringField(item, "role")
	if role == "" {
		return nil
	}
	content, hasContent := item["content"]
	if !hasContent {
		if t, ok := item["text"]; ok {
			content = t
			hasContent = true
		}
	}
	if !hasContent || content == nil {
		if calls, ok := arrayField(item, "tool_calls"); !ok || len(calls) == 0 {
			return nil
		}
	}
	msg := map[string]any{"role": role, "content": normalizeContentParts(content, stripImages)}
	if calls, ok := item["tool_calls"]; ok {
		msg["tool_calls"] = calls
	}
	return msg
}

func normalizeContentParts(content any, stripImages bool) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}
	var normalized []any
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			normalized = append(normalized, raw)
			continue
		}
		partType := stringField(part, "type")
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "input_text", "text":
			if text := stringField(part, "text"); text != "" {
				normalized = append(normalized, map[string]any{"type": "text", "text": text})
			}
		case "input_image", "image_file", "image_url":
			if stripImages {
				normalized = append(normalized, map[string]any{
					"type": "text",
					"text": imageOmittedPlaceholder,
				})
			} else {
				normalized = append(normalized, part)
			}
		default:
			normalized = append(normalized, part)
		}
	}
	if len(normalized) == 1 {
		if m, ok := normalized[0].(map[string]any); ok {
			if text := stringField(m, "text"); text != "" {
				return text
			}
		}
		return normalized[0]
	}
	if len(normalized) == 0 {
		return content
	}
	return normalized
}

func extractCallID(item map[string]any) string {
	if id := stringField(item, "call_id"); id != "" {
		return id
	}
	return stringField(item, "id")
}

func functionCallToToolCall(item map[string]any, synthIDCounter *int) map[string]any {
	callID := extractCallID(item)
	if callID == "" {
		*synthIDCounter++
		callID = "kimi_call_" + itoa(*synthIDCounter)
	}
	name := stringField(item, "name")
	if name == "" {
		name = "unknown"
	}
	var arguments any
	if args, ok := item["arguments"]; ok {
		if _, isStr := args.(string); isStr {
			arguments = args
		} else {
			arguments = toJSONString(args)
		}
	} else {
		arguments = "{}"
	}
	return map[string]any{
		"id":   callID,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}
}

func functionCallOutputToMessage(item map[string]any, fallbackID string) map[string]any {
	callID := extractCallID(item)
	if callID == "" {
		callID = fallbackID
	}
	if callID == "" {
		return nil
	}
	output := item["output"]
	if output == nil {
		output = item["content"]
	}
	if output == nil {
		output = ""
	}
	var content any
	if _, ok := output.(string); ok {
		content = output
	} else {
		content = toJSONString(output)
	}
	return map[string]any{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      content,
	}
}

// RepairToolCallIDs forces tool message tool_call_id values to pair with the
// preceding assistant tool_calls (sequential). Used after sanitize and on
// upstream 400 retry. Returns true if any repair was applied.
func RepairToolCallIDs(body map[string]any) bool {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return false
	}
	repaired := false
	var assistantCallIDs []string
	for i, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := stringField(msg, "role")
		switch role {
		case "assistant":
			assistantCallIDs = nil
			if calls, ok := arrayField(msg, "tool_calls"); ok {
				for _, c := range calls {
					if cm, ok := c.(map[string]any); ok {
						if id := stringField(cm, "id"); id != "" {
							assistantCallIDs = append(assistantCallIDs, id)
						}
					}
				}
			}
		case "tool":
			currentID := stringField(msg, "tool_call_id")
			if len(assistantCallIDs) > 0 {
				found := false
				for _, id := range assistantCallIDs {
					if id == currentID {
						found = true
						break
					}
				}
				if !found {
					msg["tool_call_id"] = assistantCallIDs[0]
					repaired = true
				}
				assistantCallIDs = assistantCallIDs[1:]
			}
			msgs[i] = msg
		}
	}
	body["messages"] = msgs
	return repaired
}

func seedProbeMessageIfNeeded(body map[string]any) bool {
	tools, ok := arrayField(body, "tools")
	if !ok || len(tools) == 0 || !messagesEmptyOrMissing(body) {
		return false
	}
	body["messages"] = []any{map[string]any{"role": "user", "content": probeUserContent}}
	return true
}

func toJSONString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
