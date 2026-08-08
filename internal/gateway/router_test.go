package gateway

import (
	"encoding/json"
	"fmt"
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
			if result.OverrideModel != defaultFlashModel {
				t.Errorf("%q override model = %q, want default %q",
					model, result.OverrideModel, defaultFlashModel)
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
	if stringField(body, "model") != defaultFlashModel {
		t.Errorf("expected default %q, got %q", defaultFlashModel, stringField(body, "model"))
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

// TestPerTurnDowngrade_ToolResultSmallReadOnly verifies that a tool-result
// turn with a small READ-ONLY tool result gets downgraded to flash EVEN when
// the content class is "editing" (which would normally keep the model). This
// is the core per-turn routing behavior: the model just received a grep/read
// result and needs to decide the next step — a task flash can handle.
//
// Note: small WRITE-tool results (StrReplace/Shell) no longer downgrade to
// flash — they route to pro because deciding the next step after a state
// change needs pro reasoning (see TestPerTurnDowngrade_ToolResultSmallWritePro).
func TestPerTurnDowngrade_ToolResultSmallReadOnly(t *testing.T) {
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module and add tests"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "Grep",
						"arguments": "{\"pattern\": \"auth\"}",
					},
				},
			}},
			// Small tool result: "grep found 3 matches", ~30 chars
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "auth.go:1:package auth\nauth.go:10:func Login()\nauth.go:20:func Logout()"},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if !result.OverrideApplied {
		t.Errorf("expected override applied for small read-only tool-result turn, got none (class=%q, turn=%q, size=%q, tool=%q)",
			result.RequestClass, result.TurnType, result.ToolResultSize, result.ToolName)
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
	if result.ToolName != "Grep" {
		t.Errorf("expected tool_name=Grep, got %q", result.ToolName)
	}
}

// TestPerTurnDowngrade_ToolResultSmallWritePro verifies that a tool-result
// turn with a small WRITE/decision tool result (StrReplace/Shell/etc.) routes
// to pro, NOT flash. Deciding the next step after a state change (did the edit
// apply correctly? what's the next file?) needs pro reasoning; downgrading to
// flash here caused agent-loop flailing in real A/B runs (more calls, more
// output, more time).
func TestPerTurnDowngrade_ToolResultSmallWritePro(t *testing.T) {
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module and add tests"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "StrReplace",
						"arguments": "{}",
					},
				},
			}},
			// Small tool result: "1 replacement made", ~24 chars
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "The file has been updated."},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if result.TurnType != TurnToolResult {
		t.Errorf("expected turn_type=tool_result, got %q", result.TurnType)
	}
	if result.ToolResultSize != "small" {
		t.Errorf("expected tool_result_size=small, got %q", result.ToolResultSize)
	}
	if result.ToolName != "StrReplace" {
		t.Errorf("expected tool_name=StrReplace, got %q", result.ToolName)
	}
	// Pro override is a no-op when the original model is already deepseek-v4-pro:
	// overrideModelForTier resolves "deepseek-v4-pro" → "deepseek-v4-pro", so
	// OverrideApplied stays false. What matters is that the tier is pro, not flash.
	if result.OverrideTier != TierPro {
		t.Errorf("expected override_tier=pro for small write-tool result, got %q", result.OverrideTier)
	}
	if result.OverrideModel == "deepseek-v4-flash" {
		t.Errorf("small write-tool result must NOT downgrade to flash; got override_model=%q", result.OverrideModel)
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

// TestPerTurnDowngrade_ToolResultMediumWriteToPro verifies the decision-aware
// tier: a MEDIUM tool result from a write/decision tool (StrReplace) on an
// expensive original model (glm-5.2) downgrades to deepseek-v4-pro (not flash),
// and does NOT apply the flash verbosity cap.
func TestPerTurnDowngrade_ToolResultMediumWriteToPro(t *testing.T) {
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":       "call_edit",
						"type":     "function",
						"function": map[string]any{"name": "StrReplace", "arguments": "{}"},
					},
				},
			},
			// Medium tool result: <4096 chars, write result
			map[string]any{"role": "tool", "tool_call_id": "call_edit", "content": strings.Repeat("y", 2000)},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if !result.OverrideApplied {
		t.Fatalf("expected override for medium write tool-result turn (class=%q, turn=%q, size=%q, tool=%q)",
			result.RequestClass, result.TurnType, result.ToolResultSize, result.ToolName)
	}
	if result.OverrideTier != TierPro {
		t.Errorf("expected override_tier=pro, got %q", result.OverrideTier)
	}
	if result.OverrideModel != "deepseek-v4-pro" {
		t.Errorf("expected deepseek-v4-pro for medium write tool result, got %q", result.OverrideModel)
	}
	if result.ToolName != "StrReplace" {
		t.Errorf("expected tool_name=StrReplace, got %q", result.ToolName)
	}
	// Pro tier must NOT receive the flash verbosity cap.
	switch v := body["max_tokens"].(type) {
	case json.Number:
		if n, _ := v.Int64(); n == toolVerbosityCap {
			t.Errorf("pro tier must not cap max_tokens to %d, got %d", toolVerbosityCap, n)
		}
	case float64:
		if int(v) == toolVerbosityCap {
			t.Errorf("pro tier must not cap max_tokens to %d, got %v", toolVerbosityCap, v)
		}
	}
}

// TestPerTurnDowngrade_ToolResultMediumReadToFlash verifies a MEDIUM read-only
// tool result (Read) downgrades to flash and gets the verbosity cap.
func TestPerTurnDowngrade_ToolResultMediumReadToFlash(t *testing.T) {
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":       "call_read",
						"type":     "function",
						"function": map[string]any{"name": "Read", "arguments": "{}"},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_read", "content": strings.Repeat("z", 2000)},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	if !result.OverrideApplied {
		t.Fatalf("expected override for medium read tool-result turn (turn=%q, size=%q, tool=%q)",
			result.TurnType, result.ToolResultSize, result.ToolName)
	}
	if result.OverrideTier != TierFlash {
		t.Errorf("expected override_tier=flash, got %q", result.OverrideTier)
	}
	if result.OverrideModel != "deepseek-v4-flash" {
		t.Errorf("expected deepseek-v4-flash for medium read tool result, got %q", result.OverrideModel)
	}
	// Flash tier must cap max_tokens on the body.
	var capVal int
	switch v := body["max_tokens"].(type) {
	case int:
		capVal = v
	case json.Number:
		n, _ := v.Int64()
		capVal = int(n)
	case float64:
		capVal = int(v)
	}
	if capVal != toolVerbosityCap {
		t.Errorf("expected body max_tokens capped to %d, got %d", toolVerbosityCap, capVal)
	}
}

// TestPerTurnVerbosityCap_AppliedEvenWhenModelOverrideNoop verifies that when
// the flow already runs on flash (so the model override is a no-op), a small
// read-only tool-result turn still receives the flash verbosity cap.
func TestPerTurnVerbosityCap_AppliedEvenWhenModelOverrideNoop(t *testing.T) {
	body := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("x", 15000)},
			map[string]any{"role": "user", "content": "refactor the auth module"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":       "call_read",
						"type":     "function",
						"function": map[string]any{"name": "Read", "arguments": "{}"},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_read", "content": "ok"},
		},
	}
	r := NewSmartRouter(RouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")

	// Override is a no-op (flash → flash) but the cap should still be applied.
	if result.OverrideApplied {
		t.Errorf("expected NO override (flash → flash no-op), got override to %q", result.OverrideModel)
	}
	var capVal int
	switch v := body["max_tokens"].(type) {
	case int:
		capVal = v
	case json.Number:
		n, _ := v.Int64()
		capVal = int(n)
	case float64:
		capVal = int(v)
	}
	if capVal != toolVerbosityCap {
		t.Errorf("expected body max_tokens capped to %d on flash no-op turn, got %d", toolVerbosityCap, capVal)
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

// TestOverrideTier covers the decision-aware per-turn and content routing tier
// logic. Tool-result turns route cheap (small / medium read-only) results to
// flash, decision-heavy (small / medium write/error) results to pro, and large
// results to keep. Non-tool turns follow the content classifier.
func TestOverrideTier(t *testing.T) {
	tests := []struct {
		name   string
		result ClassifierResult
		body   map[string]any
		want   OverrideTier
	}{
		{"tool small read → flash", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: "Read"}, nil, TierFlash},
		{"tool small grep → flash", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: "Grep"}, nil, TierFlash},
		{"tool small glob → flash", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: "Glob"}, nil, TierFlash},
		{"tool small write → pro", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: "StrReplace"}, nil, TierPro},
		{"tool small shell → pro", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: "Shell"}, nil, TierPro},
		{"tool small unknown tool → pro (write default)", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: "CustomThing"}, nil, TierPro},
		{"tool small empty tool name → pro (conservative default)", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "small", ToolName: ""}, nil, TierPro},
		{"tool medium read → flash", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: "Read"}, nil, TierFlash},
		{"tool medium grep → flash", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: "Grep"}, nil, TierFlash},
		{"tool medium glob → flash", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: "Glob"}, nil, TierFlash},
		{"tool medium write → pro", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: "StrReplace"}, nil, TierPro},
		{"tool medium shell → pro", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: "Shell"}, nil, TierPro},
		{"tool medium unknown tool → pro (write default)", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: "CustomThing"}, nil, TierPro},
		{"tool medium empty tool name → pro (conservative default)", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "medium", ToolName: ""}, nil, TierPro},
		{"tool large → keep", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "large", ToolName: "Shell"}, nil, TierKeep},
		{"tool empty size → keep", ClassifierResult{TurnType: TurnToolResult, ToolResultSize: "", ToolName: "Read"}, nil, TierKeep},
		{"content simple lookup → flash", ClassifierResult{TurnType: TurnUserPrompt, RequestClass: ClassSimpleLookup}, nil, TierFlash},
		{"content code search → flash", ClassifierResult{TurnType: TurnUserPrompt, RequestClass: ClassCodeSearch}, nil, TierFlash},
		{"content editing → keep", ClassifierResult{TurnType: TurnUserPrompt, RequestClass: ClassEditing}, nil, TierKeep},
		{"content complex reasoning → keep", ClassifierResult{TurnType: TurnUserPrompt, RequestClass: ClassComplexReasoning}, nil, TierKeep},
		{"content unknown → keep", ClassifierResult{TurnType: TurnUserPrompt, RequestClass: ClassUnknown}, nil, TierKeep},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// overrideTier uses result fields only; body is unused today but kept
			// in the signature for future signals.
			if got := overrideTier(tt.result, tt.body); got != tt.want {
				t.Errorf("overrideTier(%+v) = %q, want %q", tt.result, got, tt.want)
			}
		})
	}
}

// TestIsWriteTool verifies the read-only vs write/decision tool classification.
func TestIsWriteTool(t *testing.T) {
	readOnly := []string{"Read", "Grep", "Glob", "WebSearch", "WebFetch", "FetchMcpResource"}
	for _, name := range readOnly {
		if isWriteTool(name) {
			t.Errorf("isWriteTool(%q) = true, want false (read-only)", name)
		}
	}

	writeTools := []string{"Shell", "StrReplace", "EditNotebook", "Write", "Delete", "", "CustomTool"}
	for _, name := range writeTools {
		if !isWriteTool(name) {
			t.Errorf("isWriteTool(%q) = false, want true (write/decision/unknown)", name)
		}
	}
}

// TestToolResultName verifies we can resolve the function name for the last
// tool-result message from the preceding assistant tool_calls entry.
func TestToolResultName(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "do the thing"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_read",
						"type": "function",
						"function": map[string]any{
							"name":      "Read",
							"arguments": "{}",
						},
					},
					map[string]any{
						"id":   "call_shell",
						"type": "function",
						"function": map[string]any{
							"name":      "Shell",
							"arguments": "{}",
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_shell", "content": "exit 0"},
		},
	}
	if got := toolResultName(body); got != "Shell" {
		t.Errorf("toolResultName() = %q, want Shell", got)
	}

	// Empty messages → "".
	if got := toolResultName(map[string]any{"messages": []any{}}); got != "" {
		t.Errorf("toolResultName(empty) = %q, want empty", got)
	}

	// Not a tool-result last message → "".
	noTool := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	if got := toolResultName(noTool); got != "" {
		t.Errorf("toolResultName(no tool) = %q, want empty", got)
	}
}

// TestOverrideModelForTier verifies tier→model resolution across the maps and
// default fallbacks, including the pro/no-op cases.
func TestOverrideModelForTier(t *testing.T) {
	tests := []struct {
		name     string
		original string
		tier     OverrideTier
		want     string
	}{
		{"flash deepseek-pro → flash", "deepseek-v4-pro", TierFlash, "deepseek-v4-flash"},
		{"flash unknown → default flash", "kimi-k3", TierFlash, "deepseek-v4-flash"},
		{"pro glm-5.2 → deepseek-pro", "glm-5.2", TierPro, "deepseek-v4-pro"},
		{"pro kimi-k3 → deepseek-pro", "kimi-k3", TierPro, "deepseek-v4-pro"},
		{"pro deepseek-pro → deepseek-pro (no-op)", "deepseek-v4-pro", TierPro, "deepseek-v4-pro"},
		{"pro unknown → default pro", "thaura", TierPro, "deepseek-v4-pro"},
		{"keep deepseek-pro → unchanged", "deepseek-v4-pro", TierKeep, "deepseek-v4-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := overrideModelForTier(tt.original, tt.tier); got != tt.want {
				t.Errorf("overrideModelForTier(%q, %q) = %q, want %q", tt.original, tt.tier, got, tt.want)
			}
		})
	}
}

// TestShouldDumpForDiagnostics validates the targeted diagnostic-dump triggers.
// The full-message dump is always operator-gated by --diagnostic-dump; this
// function only narrows which requests within that session produce a file.
func TestShouldDumpForDiagnostics(t *testing.T) {
	tests := []struct {
		name           string
		turnType       TurnType
		toolResultSize string
		wouldDowngrade bool
		lastMsgLen     int
		strippedLen    int
		requestID      string
		want           bool
	}{
		{"extraction failed (stripped_len 0) → dump", TurnUserPrompt, "", false, 100, 0, "req_a", true},
		{"tool-result turn small → dump (schema validation)", TurnToolResult, "small", true, 100, 40, "req_b", true},
		{"tool-result turn large → dump", TurnToolResult, "large", false, 100, 40, "req_c", true},
		{"would downgrade → dump", TurnUserPrompt, "", true, 100, 40, "req_d", true},
		{"no-op extraction (stripped==last, non-zero) → dump", TurnUserPrompt, "", false, 40, 40, "req_e", true},
		// A user_prompt that extracted fine with no override hits only the 1%
		// deterministic sample, which is not guaranteed true for a fixed ID, so
		// we do not assert it here; see TestShouldDumpForDiagnostics_Sample.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDumpForDiagnostics(tt.turnType, tt.toolResultSize, tt.wouldDowngrade, tt.lastMsgLen, tt.strippedLen, tt.requestID); got != tt.want {
				t.Errorf("shouldDumpForDiagnostics(%q, %q, %v, %d, %d, %q) = %v, want %v",
					tt.turnType, tt.toolResultSize, tt.wouldDowngrade, tt.lastMsgLen, tt.strippedLen, tt.requestID, got, tt.want)
			}
		})
	}
}

// TestShouldDumpForDiagnostics_Sample verifies the deterministic 1%-sample
// path: the same requestID always yields the same decision (tokenized by FNV,
// not by a clock or counter), and at most 1-in-100 of distinct IDs dump on the
// sample branch alone.
func TestShouldDumpForDiagnostics_Sample(t *testing.T) {
	// Same ID, twice → same result (deterministic, no per-process state).
	id := "req_deterministic_sample"
	a := shouldDumpForDiagnostics(TurnUserPrompt, "", false, 100, 40, id)
	b := shouldDumpForDiagnostics(TurnUserPrompt, "", false, 100, 40, id)
	if a != b {
		t.Fatalf("sample decision not deterministic for same requestID: %v vs %v", a, b)
	}

	// Distinct IDs: count how many of 10k hit the sample branch only.
	var sampled int
	for i := 0; i < 10000; i++ {
		if shouldDumpForDiagnostics(TurnUserPrompt, "", false, 100, 40, fmt.Sprintf("req_sample_%d", i)) {
			sampled++
		}
	}
	// Allow slack for the exact 1% boundary across the modulus; require it to be
	// on the order of 1% (roughly 100 +/- 30), not 0 or 50%+.
	if sampled < 70 || sampled > 130 {
		t.Fatalf("deterministic sample rate off: got %d/10000 (~%0.2f%%), want ~1%%", sampled, float64(sampled)*0.01)
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
