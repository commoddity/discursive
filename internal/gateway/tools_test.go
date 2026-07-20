package gateway

import "testing"

func TestSanitizeFunctionName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "mcp dotted", raw: "mcp.filesystem.read_file"},
		{name: "short", raw: "ab"},
		{name: "invalid prefix", raw: "123tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFunctionName(tt.raw)
			if len(got) < minFunctionNameLen || len(got) > maxFunctionNameLen {
				t.Fatalf("len %d: %q", len(got), got)
			}
			if got[0] != '_' && (got[0] < 'A' || got[0] > 'z') {
				t.Fatalf("bad prefix: %q", got)
			}
		})
	}
}

func TestSanitizeSchema_DefinitionsToDefs(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"definitions": map[string]any{
			"Foo": map[string]any{"type": "string"},
		},
		"properties": map[string]any{
			"bar": map[string]any{"$ref": "#/definitions/Foo"},
		},
		"$schema": "http://json-schema.org/draft-07/schema#",
		"strict":  true,
	}
	SanitizeSchema(schema)
	if _, ok := schema["definitions"]; ok {
		t.Fatal("definitions should be removed")
	}
	defs := schema["$defs"].(map[string]any)
	if defs["Foo"] == nil {
		t.Fatal("Foo missing in $defs")
	}
	bar := schema["properties"].(map[string]any)["bar"].(map[string]any)
	if bar["$ref"] != "#/$defs/Foo" {
		t.Fatalf("$ref: %v", bar["$ref"])
	}
}

func TestSanitizeRequest_ToolNamesAndSchema(t *testing.T) {
	body := map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "mcp.filesystem.read_file",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"mode": map[string]any{"enum": []any{"a", "b"}},
						},
					},
				},
			},
			map[string]any{
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Apply a patch",
			},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	tools := res.Body["tools"].([]any)

	// Find the read_file tool by name (sorting may reorder).
	var readFileFn map[string]any
	for _, t := range tools {
		m := t.(map[string]any)
		if fn, ok := mapField(m, "function"); ok {
			name := stringField(fn, "name")
			if name != "" && name != "apply_patch" {
				readFileFn = fn
				break
			}
		}
	}
	if readFileFn == nil {
		t.Fatal("could not find read_file tool after sorting")
	}

	n0 := stringField(readFileFn, "name")
	if n0 == "" || containsRune(n0, '.') {
		t.Fatalf("sanitized name: %q", n0)
	}
	params, _ := mapField(readFileFn, "parameters")
	if params == nil {
		t.Fatal("parameters missing")
	}
	props, _ := mapField(params, "properties")
	if props == nil {
		t.Fatal("properties missing")
	}
	mode, ok := props["mode"].(map[string]any)
	if !ok || mode == nil {
		t.Fatal("mode property missing")
	}
	if mode["type"] != "string" {
		t.Fatalf("enum type inference: %v", mode)
	}
}

func TestSanitizeRequest_ToolPairingOrphan(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4o-mini",
		"messages": []any{
			map[string]any{"role": "tool", "content": map[string]any{"result": "ok"}, "tool_call_id": "1"},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.Body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "assistant" {
		t.Fatal("expected synthetic assistant")
	}
	tool := msgs[1].(map[string]any)
	if tool["role"] != "tool" {
		t.Fatal("expected tool message")
	}
}

func TestSanitizeRequest_RemapsToolCallNames(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4o-mini",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id": "call_1", "type": "function",
						"function": map[string]any{"name": "mcp.foo.bar", "arguments": "{}"},
					},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "mcp.foo.bar",
					"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}
	res, err := SanitizeRequest(body, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	toolName := res.Body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
	callName := res.Body["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
	if toolName != callName {
		t.Fatalf("names differ: tool=%q call=%q", toolName, callName)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
