package gateway

// injectZaiLanguageDirective appends the English-only pin to the last
// system/developer message (prepending a system message when none exists).
func (s *Server) injectZaiLanguageDirective(body map[string]any) {
	directive := map[string]any{"role": "system", "content": languageDirective}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		switch c := m["content"].(type) {
		case string:
			m["content"] = c + "\n\n" + languageDirective
		case []any:
			m["content"] = append(c, map[string]any{"type": "text", "text": languageDirective})
		default:
			m["content"] = languageDirective
		}
		return
	}
	body["messages"] = append([]any{directive}, msgs...)
}
