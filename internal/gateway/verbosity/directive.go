package verbosity

// injectDirective appends directive to the last system (or developer) message
// in body["messages"]. If no system/developer message exists, it prepends a
// new system message. It mutates body in place. body may have a nil messages
// field (no-op).
func injectDirective(body map[string]any, directive string) {
	if directive == "" {
		return
	}
	msgs, ok := body["messages"].([]any)
	if !ok {
		// No messages array — nothing to target. Leave untouched.
		return
	}
	if len(msgs) == 0 {
		// Empty messages — nothing to target. Leave untouched.
		return
	}

	// Find the last system/developer message and append the directive to it.
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		appendToMessageContent(m, directive)
		body["messages"] = msgs
		return
	}

	// No system message: prepend a new one so the directive is still applied
	// as an instruction overlay (recency-weighted).
	prepend := map[string]any{"role": "system", "content": directive}
	body["messages"] = append([]any{prepend}, msgs...)
}

// appendToMessageContent appends directive to a message's content, which may
// be a plain string or an array of content parts. It normalizes to a string,
// appending the directive on a new line.
func appendToMessageContent(m map[string]any, directive string) {
	switch c := m["content"].(type) {
	case string:
		if c == "" {
			m["content"] = directive
		} else {
			m["content"] = c + "\n\n" + directive
		}
	case []any:
		// Append a new text part so we never disturb existing image/parts.
		m["content"] = append(c, map[string]any{"type": "text", "text": directive})
	default:
		// Unknown content shape — create a string content with the directive.
		m["content"] = directive
	}
}
