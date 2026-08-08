package gateway

import (
	"strings"
	"testing"
)

func TestSmartRouter_SubagentDetection(t *testing.T) {
	tests := []struct {
		name         string
		body         map[string]any
		wantSubagent bool
		wantClass    RequestClass
		wantOverride string // empty = no override expected
		enabled      bool
	}{
		{
			name: "subagent: short system prompt + few messages + tools → downgrade",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": "You are a code researcher. Never modify files."},
					map[string]any{"role": "user", "content": "Find all SQL queries in this project."},
				},
				"tools": []any{
					map[string]any{"type": "function", "function": map[string]any{"name": "grep"}},
				},
			},
			enabled:      true,
			wantSubagent: false,           // subagent detection disabled
			wantClass:    ClassCodeSearch, // content-based: "find" keyword
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "subagent: all 3 signals → short prompt + 3 msgs + tools",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": "Explore the codebase. Read only. Never modify files."},
					map[string]any{"role": "user", "content": "map the architecture"},
				},
				"tools": []any{
					map[string]any{"type": "function", "function": map[string]any{"name": "read_file"}},
					map[string]any{"type": "function", "function": map[string]any{"name": "list_dir"}},
				},
			},
			enabled:      true,
			wantSubagent: false,                 // subagent detection disabled
			wantClass:    ClassComplexReasoning, // content-based: "architecture" keyword
			wantOverride: "",                    // complex reasoning keeps model
		},
		{
			name: "main agent: long system prompt + many messages → no override",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Refactor this function"},
					map[string]any{"role": "assistant", "content": "OK, let me look at it."},
					map[string]any{"role": "user", "content": "Actually also fix the tests"},
					map[string]any{"role": "assistant", "content": "Sure, here's the fix."},
					map[string]any{"role": "user", "content": "Now the lint error above"},
					map[string]any{"role": "assistant", "content": "Fixed."},
					map[string]any{"role": "user", "content": "Implement the missing error handler"},
				},
				"tools": []any{
					map[string]any{"type": "function", "function": map[string]any{"name": "read_file"}},
				},
			},
			enabled:      true,
			wantSubagent: false,
			wantClass:    ClassEditing, // "Refactor this function" → editing
			wantOverride: "",
		},
		{
			name: "main agent: long system prompt → only 1 signal matches → no override",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "hi"},
				},
			},
			enabled:      true,
			wantSubagent: false,
			wantClass:    ClassSimpleLookup, // "hi" ≤ 120 chars → simple lookup
			wantOverride: "deepseek-v4-flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewSmartRouter(RouterConfig{Enabled: tt.enabled})
			result := r.ClassifyAndOverride(tt.body, "req_test")

			if result.IsSubagent != tt.wantSubagent {
				t.Errorf("IsSubagent = %v, want %v (signals: sys=%d msg=%d tools=%v)",
					result.IsSubagent, tt.wantSubagent,
					result.SysPromptLen, result.MsgCount, result.HasTools)
			}

			if result.RequestClass != tt.wantClass {
				t.Errorf("RequestClass = %q, want %q", result.RequestClass, tt.wantClass)
			}

			if tt.wantOverride != "" {
				if !result.OverrideApplied {
					t.Errorf("OverrideApplied = false, want true")
				}
				if tt.body != nil {
					gotModel := stringField(tt.body, "model")
					if gotModel != tt.wantOverride {
						t.Errorf("body.model = %q, want %q", gotModel, tt.wantOverride)
					}
				}
				if result.OverrideModel != tt.wantOverride {
					t.Errorf("OverrideModel = %q, want %q", result.OverrideModel, tt.wantOverride)
				}
			} else {
				if result.OverrideApplied {
					t.Errorf("OverrideApplied = true, want false (OverrideModel=%q)", result.OverrideModel)
				}
			}

			if result.ClassificationAge != "v2" {
				t.Errorf("ClassificationAge = %q, want %q", result.ClassificationAge, "v2")
			}
		})
	}
}

func TestContentClassification_DowngradePath(t *testing.T) {
	tests := []struct {
		name         string
		body         map[string]any
		wantClass    RequestClass
		wantOverride string // empty = no override
	}{
		{
			name: "simple lookup → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "What is the difference between goroutines and threads?"},
				},
			},
			wantClass:    ClassSimpleLookup,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "explain prefix → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Explain how the Go scheduler works"},
				},
			},
			wantClass:    ClassSimpleLookup,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "how does prefix → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "How does the gateway sanitizer work?"},
				},
			},
			wantClass:    ClassSimpleLookup,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "code search: find keyword → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Find all tests that use the pricing table"},
				},
			},
			wantClass:    ClassCodeSearch,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "code search: search keyword → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Search for usages of the api_key field"},
				},
			},
			wantClass:    ClassCodeSearch,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "code search: explore keyword → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Explore the internal/config directory"},
				},
			},
			wantClass:    ClassCodeSearch,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "structured extraction: json_object → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Extract all function names from this file"},
				},
				"response_format": map[string]any{"type": "json_object"},
			},
			wantClass:    ClassStructuredExtraction,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "structured extraction: json_schema → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "List all available models with their prices"},
				},
				"response_format": map[string]any{"type": "json_schema"},
			},
			wantClass:    ClassStructuredExtraction,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "automation: pull request → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Open a pull request with the current work"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "automation: git push → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Git commit and push the current branch"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "automation: gh pr → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "gh pr create --title 'Add feature X'"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "automation: run script → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Run the release script"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "automation: run a script → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Run a deployment script"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "editing+complex: refactor + pipeline → keep model (complex beats editing)",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Refactor the sanitizer to use a pipeline pattern"},
				},
			},
			wantClass:    ClassComplexReasoning, // pipeline → complex beats refactor → editing
			wantOverride: "",
		},
		{
			name: "editing: fix → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Fix the nil pointer dereference in proxy.go"},
				},
			},
			wantClass:    ClassEditing,
			wantOverride: "",
		},
		{
			name: "editing: implement → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Implement a new rate limiter for the gateway"},
				},
			},
			wantClass:    ClassEditing,
			wantOverride: "",
		},
		{
			name: "complex reasoning: long multiline → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "We need a new architecture for the smart router that supports:\n1. Subagent detection\n2. Content-based classification\n3. Model override with fallback tiers"},
				},
			},
			wantClass:    ClassComplexReasoning,
			wantOverride: "",
		},
		{
			name: "complex reasoning: architecture keyword → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "What would be the best architecture for multi-provider failover?"},
				},
			},
			wantClass:    ClassComplexReasoning,
			wantOverride: "",
		},
		{
			name: "complex reasoning: finish plan → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Finish plan"},
				},
			},
			wantClass:    ClassComplexReasoning,
			wantOverride: "",
		},
		{
			name: "complex reasoning: plan keyword → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Create a plan for the database migration"},
				},
			},
			wantClass:    ClassComplexReasoning,
			wantOverride: "",
		},
		{
			name: "complex reasoning: planning keyword → keep model",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "I need help with planning the release"},
				},
			},
			wantClass:    ClassComplexReasoning,
			wantOverride: "",
		},
		{
			name: "unknown: ambiguous message → keep model (conservative)",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Let's get started"},
				},
			},
			wantClass:    ClassSimpleLookup, // ≤ 120 chars → simple lookup → downgrade
			wantOverride: "deepseek-v4-flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewSmartRouter(RouterConfig{Enabled: true})
			result := r.ClassifyAndOverride(tt.body, "req_test")

			if result.RequestClass != tt.wantClass {
				t.Errorf("RequestClass = %q, want %q", result.RequestClass, tt.wantClass)
			}

			if tt.wantOverride != "" {
				if !result.OverrideApplied {
					t.Errorf("OverrideApplied = false, want true")
				}
				if tt.body != nil {
					gotModel := stringField(tt.body, "model")
					if gotModel != tt.wantOverride {
						t.Errorf("body.model = %q, want %q", gotModel, tt.wantOverride)
					}
				}
			} else {
				if result.OverrideApplied {
					t.Errorf("OverrideApplied = true, want false (request_class=%q, OverrideModel=%q)", result.RequestClass, result.OverrideModel)
				}
			}
		})
	}
}

func TestContentClassification_OrderOfPrecedence(t *testing.T) {
	// Editing should take priority over code_search when keywords overlap.
	tests := []struct {
		name      string
		userMsg   string
		extra     map[string]any
		wantClass RequestClass
	}{
		{
			name:      "fix → editing, not code_search (despite 'check' keyword)",
			userMsg:   "Fix the bug in the search function",
			wantClass: ClassEditing,
		},
		{
			name:      "add → editing, not code_search (despite 'list' keyword in context)",
			userMsg:   "Add a new list endpoint for the API",
			wantClass: ClassEditing,
		},
		{
			name:      "structured output beats simple lookup for short message",
			userMsg:   "Extract names",
			extra:     map[string]any{"response_format": map[string]any{"type": "json_object"}},
			wantClass: ClassStructuredExtraction,
		},
		{
			name:      "refactor + pr → editing over automation (edited keyword wins for mixed)",
			userMsg:   "Refactor the sanitizer and open a pull request",
			wantClass: ClassEditing,
		},
		{
			name:      "pure automation: create PR → automation (no editing keywords)",
			userMsg:   "Open a pull request for the current branch",
			wantClass: ClassAutomation,
		},
		{
			name:      "short planning message → complex reasoning (not simple lookup)",
			userMsg:   "Finish plan",
			wantClass: ClassComplexReasoning,
		},
		{
			name:      "create a plan → complex reasoning (not simple lookup)",
			userMsg:   "Create a plan for the release",
			wantClass: ClassComplexReasoning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": tt.userMsg},
				},
			}
			if tt.extra != nil {
				for k, v := range tt.extra {
					body[k] = v
				}
			}
			result := classifyRequest(body)
			if result != tt.wantClass {
				t.Errorf("classifyRequest = %q, want %q", result, tt.wantClass)
			}
		})
	}
}

func TestSmartRouter_ModelPreservedForMainAgent(t *testing.T) {
	// Main agent: long system prompt, editing request → no override, model preserved.
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": longSystemPrompt},
			map[string]any{"role": "user", "content": "Implement a new rate limiter"},
			map[string]any{"role": "assistant", "content": "OK, let me look at the code first."},
			map[string]any{"role": "user", "content": "m2"},
			map[string]any{"role": "assistant", "content": "r2"},
			map[string]any{"role": "user", "content": "m3"},
			map[string]any{"role": "assistant", "content": "r3"},
			map[string]any{"role": "user", "content": "Add another endpoint for health checks"},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if result.OverrideApplied {
		t.Errorf("main agent editing should not be overridden, got %q → %q (class=%q)", result.OriginalModel, result.OverrideModel, result.RequestClass)
	}
	if stringField(body, "model") != "deepseek-v4-pro" {
		t.Errorf("main agent model should not be overridden, got %q", stringField(body, "model"))
	}
	if result.RequestClass != ClassEditing {
		t.Errorf("expected editing class, got %q", result.RequestClass)
	}
}

func TestSmartRouter_UnknownProviderDefaultFallback(t *testing.T) {
	// Models not in the override map fall back to deepseek-v4-flash when
	// classification triggers a downgrade.
	models := []string{"kimi-k3", "kimi-k2.7-code", "thaura"}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			body := map[string]any{
				"model": model,
				"messages": []any{
					map[string]any{"role": "system", "content": "short"},
					map[string]any{"role": "user", "content": "search for tests"},
				},
				"tools": []any{
					map[string]any{"type": "function", "function": map[string]any{"name": "grep"}},
				},
			}
			r := NewSmartRouter(RouterConfig{Enabled: true})
			result := r.ClassifyAndOverride(body, "req_test")
			if !result.OverrideApplied {
				t.Errorf("%q should be overridden (not in map → fallback default), class=%q",
					model, result.RequestClass)
			}
			if result.OverrideModel != defaultSubagentModel {
				t.Errorf("%q override model = %q, want default %q",
					model, result.OverrideModel, defaultSubagentModel)
			}
		})
	}
}

func TestSmartRouter_Glm52ToFlashViaDefault(t *testing.T) {
	// glm-5.2 is NOT in the override map (line is commented out), so it
	// falls back to the universal default (deepseek-v4-flash).
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "system", "content": longSystemPrompt},
			map[string]any{"role": "user", "content": "search for all test files"},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")
	if !result.OverrideApplied {
		t.Errorf("glm-5.2 should be overridden (not in map → default), got class=%q", result.RequestClass)
	}
	if stringField(body, "model") != defaultSubagentModel {
		t.Errorf("expected default %q, got %q", defaultSubagentModel, stringField(body, "model"))
	}
}

func TestSmartRouter_SubagentOnFlashIsNoop(t *testing.T) {
	body := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "system", "content": "short"},
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "grep"}},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")
	if result.OverrideApplied {
		t.Errorf("flash → flash should be a no-op, but OverrideApplied=true (%q → %q)", result.OriginalModel, result.OverrideModel)
	}
	if stringField(body, "model") != "deepseek-v4-flash" {
		t.Errorf("flash model should stay flash, got %q", stringField(body, "model"))
	}
}

func TestSmartRouter_DisabledPreservesModelAndClassifies(t *testing.T) {
	// When disabled, classification still runs but no override.
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": longSystemPrompt},
			map[string]any{"role": "user", "content": "What is a goroutine?"},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: false})
	result := r.ClassifyAndOverride(body, "req_test")

	if result.RequestClass != ClassSimpleLookup {
		t.Errorf("classification should still run when disabled, got %q", result.RequestClass)
	}
	if result.OverrideApplied {
		t.Errorf("disabled router should not apply override, got %q", result.OverrideModel)
	}
	if stringField(body, "model") != "deepseek-v4-pro" {
		t.Errorf("disabled router should not modify model, got %q", stringField(body, "model"))
	}
}

// TestPerTurnDowngrade_ToolResultSmall verifies that a tool-result turn with a
// small result gets downgraded to flash EVEN when the content class is
// "editing" (which would normally keep the model). This is the core per-turn
// routing behavior: the model just received a tool result and needs to decide
// the next step — a task flash can handle.
func TestPerTurnDowngrade_ToolResultSmall(t *testing.T) {
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module and add tests"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{}},
			// Small tool result: "go test" output, ~30 chars
			map[string]any{"role": "tool", "content": "ok\tinternal/gateway\t0.123s"},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if !result.OverrideApplied {
		t.Errorf("expected override applied for small tool-result turn, got none (class=%q, turn=%q, size=%q)",
			result.RequestClass, result.TurnType, result.ToolResultSize)
	}
	if result.OverrideModel != "deepseek-v4-flash" {
		t.Errorf("expected deepseek-v4-flash, got %q", result.OverrideModel)
	}
	if result.TurnType != TurnToolResult {
		t.Errorf("expected turn_type=tool_result, got %q", result.TurnType)
	}
	if result.ToolResultSize != "small" {
		t.Errorf("expected tool_result_size=small, got %q", result.ToolResultSize)
	}
}

// TestPerTurnDowngrade_ToolResultLargeKeptModel verifies that a tool-result
// turn with a LARGE result keeps the original model, even though it's a
// tool-result turn. This is the conservative policy: large results may need
// pro-level interpretation.
func TestPerTurnDowngrade_ToolResultLargeKeptModel(t *testing.T) {
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module and add tests"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{}},
			// Large tool result: >4096 chars
			map[string]any{"role": "tool", "content": strings.Repeat("x", 5000)},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if result.OverrideApplied {
		t.Errorf("expected NO override for large tool-result turn, got override to %q (class=%q, turn=%q, size=%q)",
			result.OverrideModel, result.RequestClass, result.TurnType, result.ToolResultSize)
	}
	if result.ToolResultSize != "large" {
		t.Errorf("expected tool_result_size=large, got %q", result.ToolResultSize)
	}
}

func TestShouldDowngrade(t *testing.T) {
	tests := []struct {
		class RequestClass
		want  bool
	}{
		{ClassSubagent, true},
		{ClassSimpleLookup, true},
		{ClassCodeSearch, true},
		{ClassStructuredExtraction, true},
		{ClassAutomation, true},
		{ClassEditing, false},
		{ClassComplexReasoning, false},
		{ClassUnknown, false},
		{RequestClass("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.class), func(t *testing.T) {
			if got := shouldDowngrade(tt.class); got != tt.want {
				t.Errorf("shouldDowngrade(%q) = %v, want %v", tt.class, got, tt.want)
			}
		})
	}
}

func TestShouldDowngradeTurn(t *testing.T) {
	tests := []struct {
		name           string
		turnType       TurnType
		toolResultSize string
		want           bool
	}{
		{"tool_result small → downgrade", TurnToolResult, "small", true},
		{"tool_result medium → downgrade", TurnToolResult, "medium", true},
		{"tool_result large → keep (conservative)", TurnToolResult, "large", false},
		{"tool_result empty size → keep", TurnToolResult, "", false},
		{"tool_result bogus size → keep", TurnToolResult, "enormous", false},
		{"user_prompt → keep", TurnUserPrompt, "", false},
		{"agent_continue → keep", TurnAgentContinue, "", false},
		{"unknown turn → keep", TurnUnknown, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDowngradeTurn(tt.turnType, tt.toolResultSize); got != tt.want {
				t.Errorf("shouldDowngradeTurn(%q, %q) = %v, want %v", tt.turnType, tt.toolResultSize, got, tt.want)
			}
		})
	}
}

func TestLastUserMessage(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "simple string content",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": "hello world"},
				},
			},
			want: "hello world",
		},
		{
			name: "last user message among many",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "sys"},
					map[string]any{"role": "user", "content": "first"},
					map[string]any{"role": "assistant", "content": "reply"},
					map[string]any{"role": "user", "content": "second"},
				},
			},
			want: "second",
		},
		{
			name: "array content",
			body: map[string]any{
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "text", "text": "part1"},
							map[string]any{"type": "text", "text": "part2"},
						},
					},
				},
			},
			want: "part1part2",
		},
		{
			name: "no user message",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "sys"},
				},
			},
			want: "",
		},
		{
			name: "empty messages",
			body: map[string]any{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastUserMessage(tt.body); got != tt.want {
				t.Errorf("lastUserMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// longSystemPrompt simulates a main-agent system prompt with project rules (~1200 chars).
var longSystemPrompt = `You are a senior software engineer working on the Discursive project — a local OpenAI-compatible gateway that sanitizes Cursor payloads and routes to upstream AI providers (Moonshot/Kimi, DeepSeek, Thaura, Z.AI).

## Project Rules
- Go 1.26.5+ only. No Docker by default. No npm/pnpm in the product tree.
- CLI uses Cobra; no Viper. Config lives in internal/config.
- Logging via log/slog (JSON on stdout, never secrets).
- Tests must be table-driven: []struct{ name string; ... } + t.Run.
- Never commit without explicit user approval. Never push force.
- Strip unsupported params before proxying upstream.
- Apply provider-specific thinking policies per route.

## Architecture
internal/gateway/   → HTTP server, auth, sanitizer, optimizer, proxy
internal/tunnel/    → cloudflared Quick Tunnel or BYO public URL
internal/usage/     → pricing table, per-session token/cost store
internal/config/    → settings, paths, validation

## Routing
- gpt-4o → Moonshot kimi-k3 (planning/flagship)
- gpt-4o-mini → Moonshot kimi-k2.7-code (coding)
- o1 → DeepSeek deepseek-v4-pro (hard execution)
- o3-mini → DeepSeek deepseek-v4-flash (cheap execution)
- gpt-5-nano → Thaura thaura (ethical AI)
- gpt-4.1-turbo → Z.AI glm-5.2 (planning, cheaper than K3)
- gpt-4.1 → Z.AI glm-4.7 (cheap execution)

## Coding Standards
- Wrap errors with %w; return actionable messages at CLI boundaries.
- Never suppress linter warnings with //nolint comments.
- Never log API keys or raw Authorization headers.
- Prefer small packages with package comments and one-line Contracts.`

func TestStripCursorXML(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no XML blocks",
			input:  "What is a goroutine?",
			expect: "What is a goroutine?",
		},
		{
			name:   "open_and_recently_viewed_files block stripped",
			input:  "<open_and_recently_viewed_files>\nRecently viewed files:\n- /foo/bar.go\n</open_and_recently_viewed_files>\nWhat is a goroutine?",
			expect: "What is a goroutine?",
		},
		{
			name:   "multiple XML blocks stripped",
			input:  "<attached_files>\n<code_selection path=\"foo\">\nsome code\n</code_selection>\n</attached_files>\nExplain this function",
			expect: "Explain this function",
		},
		{
			name:   "only XML, no user message",
			input:  "<open_and_recently_viewed_files>\n- foo\n</open_and_recently_viewed_files>",
			expect: "",
		},
		{
			name:   "user message with leading and trailing newlines",
			input:  "<terminal_selection>\noutput\n</terminal_selection>\n\n\nWhat is a goroutine?\n\n",
			expect: "What is a goroutine?",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCursorXML(tt.input)
			if got != tt.expect {
				t.Errorf("stripCursorXML() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestStripCursorNoise(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no noise",
			input:  "What is a goroutine?",
			expect: "What is a goroutine?",
		},
		{
			name:   "XML and summary prefix with real message after",
			input:  "<open_and_recently_viewed_files>\n- foo.go\n</open_and_recently_viewed_files>\n\nWhat is a goroutine?",
			expect: "What is a goroutine?",
		},
		{
			name:   "summary block stripped, real message preserved",
			input:  "Your actual prompt here\n\n[Previous conversation summary]: Summary:\n1. Primary Request: ...\n\n",
			expect: "Your actual prompt here",
		},
		{
			name:   "summary block at start with real message after it",
			input:  "[Previous conversation summary]: Summary:\nSome context...\n\nWhat is a goroutine?",
			expect: "What is a goroutine?",
		},
		{
			name:   "pure summary, no real message",
			input:  "[Previous conversation summary]: Summary:\n1. Primary Request: ...",
			expect: "",
		},
		{
			name:   "summary block without newline separator after",
			input:  "Some message\n\n[Previous conversation summary]: Summary:\n...",
			expect: "Some message",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCursorNoise(tt.input)
			if got != tt.expect {
				t.Errorf("stripCursorNoise() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestContentClassification_WithCursorXML(t *testing.T) {
	// Simulates real Cursor traffic where the user message is wrapped in XML blocks.
	tests := []struct {
		name         string
		body         map[string]any
		wantClass    RequestClass
		wantOverride string
	}{
		{
			name: "simple lookup wrapped in open_and_recently_viewed_files",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "<open_and_recently_viewed_files>\nRecently viewed files (recent at the top, oldest at the bottom):\n- /Users/pascal/local-code/discursive/internal/gateway/router.go (total lines: 443)\n</open_and_recently_viewed_files>\nWhat is a goroutine?"},
				},
			},
			wantClass:    ClassSimpleLookup,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "code search wrapped in attached_files",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "<attached_files>\n\n<code_selection path=\"/foo/bar.go\" lines=\"1-10\">\npackage main\nfunc main() {}\n</code_selection>\n\n</attached_files>\nFind all test files in this project"},
				},
			},
			wantClass:    ClassCodeSearch,
			wantOverride: "deepseek-v4-flash",
		},
		{
			name: "editing+complex wrapped in XML",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "<open_and_recently_viewed_files>\n- router.go\n</open_and_recently_viewed_files>\nRefactor the classifyRequest function to use a pipeline pattern"},
				},
			},
			wantClass:    ClassComplexReasoning, // pipeline → complex beats refactor → editing
			wantOverride: "",
		},
		{
			name: "complex reasoning wrapped in XML",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "<open_and_recently_viewed_files>\n- router.go\n- proxy.go\n</open_and_recently_viewed_files>\nDesign a scalable architecture for multi-provider routing with automatic failover and retry logic"},
				},
			},
			wantClass:    ClassComplexReasoning,
			wantOverride: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewSmartRouter(RouterConfig{Enabled: true})
			result := r.ClassifyAndOverride(tt.body, "req_test")

			if result.RequestClass != tt.wantClass {
				t.Errorf("RequestClass = %q, want %q", result.RequestClass, tt.wantClass)
			}

			if tt.wantOverride != "" {
				if !result.OverrideApplied {
					t.Errorf("OverrideApplied = false, want true")
				}
			} else {
				if result.OverrideApplied {
					t.Errorf("OverrideApplied = true, want false (class=%q)", result.RequestClass)
				}
			}
		})
	}
}

func TestDetectTurnType(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want TurnType
	}{
		{
			name: "tool result turn",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "You are a coding agent."},
					map[string]any{"role": "user", "content": "Run the tests"},
					map[string]any{"role": "assistant", "content": "", "tool_calls": []any{}},
					map[string]any{"role": "tool", "content": "ok\tinternal/gateway\t0.123s"},
				},
			},
			want: TurnToolResult,
		},
		{
			name: "user prompt turn",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "You are a coding agent."},
					map[string]any{"role": "user", "content": "What is a goroutine?"},
				},
			},
			want: TurnUserPrompt,
		},
		{
			name: "developer prompt turn",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "developer", "content": "You are a coding agent."},
					map[string]any{"role": "user", "content": "Hello"},
				},
			},
			want: TurnUserPrompt,
		},
		{
			name: "assistant continuation turn",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "You are a coding agent."},
					map[string]any{"role": "assistant", "content": "Let me think about the next step..."},
				},
			},
			want: TurnAgentContinue,
		},
		{
			name: "empty messages → unknown",
			body: map[string]any{"messages": []any{}},
			want: TurnUnknown,
		},
		{
			name: "no messages key → unknown",
			body: map[string]any{},
			want: TurnUnknown,
		},
		{
			name: "nil body → unknown",
			body: nil,
			want: TurnUnknown,
		},
		{
			name: "unknown role → unknown",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "Hello"},
					map[string]any{"role": "bogus", "content": "?"},
				},
			},
			want: TurnUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTurnType(tt.body)
			if got != tt.want {
				t.Errorf("detectTurnType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolResultSize(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "small tool result (≤512)",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "Agent"},
					map[string]any{"role": "tool", "content": "ok"},
				},
			},
			want: "small",
		},
		{
			name: "medium tool result (≤4096)",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "Agent"},
					map[string]any{
						"role":    "tool",
						"content": strings.Repeat("x", 1024),
					},
				},
			},
			want: "medium",
		},
		{
			name: "large tool result (>4096)",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "Agent"},
					map[string]any{
						"role":    "tool",
						"content": strings.Repeat("x", 5000),
					},
				},
			},
			want: "large",
		},
		{
			name: "boundary: exactly 512 → small",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "Agent"},
					map[string]any{
						"role":    "tool",
						"content": strings.Repeat("x", 512),
					},
				},
			},
			want: "small",
		},
		{
			name: "boundary: exactly 4096 → medium",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "system", "content": "Agent"},
					map[string]any{
						"role":    "tool",
						"content": strings.Repeat("x", 4096),
					},
				},
			},
			want: "medium",
		},
		{
			name: "not a tool turn → empty",
			body: map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": "Hello"},
				},
			},
			want: "",
		},
		{
			name: "empty messages → empty",
			body: map[string]any{"messages": []any{}},
			want: "",
		},
		{
			name: "nil body → empty",
			body: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolResultSize(tt.body)
			if got != tt.want {
				t.Errorf("toolResultSize() = %q, want %q", got, tt.want)
			}
		})
	}
}
