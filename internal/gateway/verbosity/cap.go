package verbosity

import "encoding/json"

// capTokens lowers body["max_tokens"] to at most cap. It never increases the
// requested value, so an explicit smaller budget from the client is preserved.
// It also maps max_completion_tokens into max_tokens first (mirroring the
// sanitizer), then applies the cap. body may be nil (no-op).
func capTokens(body map[string]any, cap int) {
	if body == nil || cap <= 0 {
		return
	}

	// Mirror sanitizer: if the client sent max_completion_tokens (Responses
	// API) and not max_tokens, fold it into max_tokens first.
	if _, hasMax := body["max_tokens"]; !hasMax {
		if mc, ok := body["max_completion_tokens"]; ok {
			if n, ok := numberToInt(mc); ok {
				body["max_tokens"] = n
			}
		}
	}

	if v, ok := body["max_tokens"]; ok {
		if n, ok := numberToInt(v); ok && n > cap {
			body["max_tokens"] = cap
		}
	}
}

// numberToInt coerces an int-like (int, int64, float64, json.Number) to an
// int. Values that are not numbers return ok=false.
func numberToInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}
