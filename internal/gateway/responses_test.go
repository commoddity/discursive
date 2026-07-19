package gateway

import "testing"

func TestAdaptCursorResponsesRequest(t *testing.T) {
	tests := []struct {
		name      string
		body      map[string]any
		wantLen   int
		wantRole0 string
		check     func(t *testing.T, body map[string]any)
	}{
		{
			name:    "string input",
			body:    map[string]any{"input": "Continue the build"},
			wantLen: 1,
			check: func(t *testing.T, body map[string]any) {
				msgs := body["messages"].([]any)
				m := msgs[0].(map[string]any)
				if m["role"] != "user" || m["content"] != "Continue the build" {
					t.Fatalf("msg: %v", m)
				}
			},
		},
		{
			name: "instructions and roles",
			body: map[string]any{
				"instructions": "You are a coding agent.",
				"input": []any{
					map[string]any{"role": "developer", "content": "Be concise."},
					map[string]any{"role": "user", "content": "Build a CLI todo app"},
					map[string]any{"role": "assistant", "content": "I'll create the project structure."},
					map[string]any{"role": "user", "content": "Continue the build"},
				},
			},
			wantLen: 5,
			check: func(t *testing.T, body map[string]any) {
				msgs := body["messages"].([]any)
				if msgs[0].(map[string]any)["role"] != "system" {
					t.Fatal("expected system")
				}
				if msgs[4].(map[string]any)["content"] != "Continue the build" {
					t.Fatal("last content mismatch")
				}
			},
		},
		{
			name: "function call items",
			body: map[string]any{
				"input": []any{
					map[string]any{"role": "user", "content": "read package.json"},
					map[string]any{
						"type": "function_call", "call_id": "call_abc",
						"name": "read_file", "arguments": `{"path":"package.json"}`,
					},
					map[string]any{
						"type": "function_call_output", "call_id": "call_abc",
						"output": `{"name":"my-app"}`,
					},
					map[string]any{"role": "user", "content": "Continue"},
				},
			},
			wantLen: 4,
			check: func(t *testing.T, body map[string]any) {
				msgs := body["messages"].([]any)
				if msgs[1].(map[string]any)["role"] != "assistant" {
					t.Fatal("expected assistant tool_calls")
				}
				if msgs[2].(map[string]any)["tool_call_id"] != "call_abc" {
					t.Fatal("tool_call_id mismatch")
				}
			},
		},
		{
			name: "existing messages untouched",
			body: map[string]any{
				"messages": []any{map[string]any{"role": "user", "content": "hello"}},
				"input":    "ignored",
			},
			wantLen: 1,
			check: func(t *testing.T, body map[string]any) {
				msgs := body["messages"].([]any)
				if msgs[0].(map[string]any)["content"] != "hello" {
					t.Fatal("messages changed")
				}
				if _, ok := body["input"]; ok {
					t.Fatal("input should be removed")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptCursorResponsesRequest(tt.body, false)
			msgs, ok := tt.body["messages"].([]any)
			if !ok {
				t.Fatal("no messages")
			}
			if len(msgs) != tt.wantLen {
				t.Fatalf("len: got %d want %d", len(msgs), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, tt.body)
			}
		})
	}
}

func TestSeedProbeMessageIfNeeded(t *testing.T) {
	body := map[string]any{
		"tools": []any{probeTool()},
	}
	if !seedProbeMessageIfNeeded(body) {
		t.Fatal("expected seed")
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != probeUserContent {
		t.Fatalf("probe: %v", msgs)
	}
}

func TestRepairToolCallIDs(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{"id": "call_a", "type": "function", "function": map[string]any{"name": "x", "arguments": "{}"}},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "wrong", "content": "ok"},
		},
	}
	if !RepairToolCallIDs(body) {
		t.Fatal("expected repair")
	}
	msgs := body["messages"].([]any)
	tool := msgs[1].(map[string]any)
	if tool["tool_call_id"] != "call_a" {
		t.Fatalf("repaired id: %v", tool["tool_call_id"])
	}
}

func TestPairsOutputWithoutCallID(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{"role": "user", "content": "run the tool"},
			map[string]any{"type": "function_call", "call_id": "call_xyz", "name": "run_cmd", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "output": "done"},
		},
	}
	adaptCursorResponsesRequest(body, false)
	msgs := body["messages"].([]any)
	tool := msgs[2].(map[string]any)
	if tool["tool_call_id"] != "call_xyz" {
		t.Fatalf("paired id: %v", tool["tool_call_id"])
	}
}
