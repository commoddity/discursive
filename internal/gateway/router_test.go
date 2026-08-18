package gateway

import (
	"testing"
)

func TestSubAgentRouter_SubagentDetection(t *testing.T) {
	tests := []struct {
		name         string
		body         map[string]any
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
			wantClass:    ClassCodeSearch, // content-based: "find" keyword
			wantOverride: "glm-4.7",
		},
		{
			name: "subagent: all 3 signals → short prompt + 3 msgs + tools",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": "Explore the codebase. Read only. Never modify files."},
					map[string]any{"role": "user", "content": "How would you design the system architecture for this app?"},
				},
				"tools": []any{
					map[string]any{"type": "function", "function": map[string]any{"name": "read_file"}},
					map[string]any{"type": "function", "function": map[string]any{"name": "list_dir"}},
				},
			},
			enabled:      true,
			wantClass:    ClassComplexReasoning, // content-based: "how would you" prefix
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
			wantClass:    ClassUnknown, // short but no recognizable prefix → unknown (conservative)
			wantOverride: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: tt.enabled})
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
				if result.OverrideModel != tt.wantOverride {
					t.Errorf("OverrideModel = %q, want %q", result.OverrideModel, tt.wantOverride)
				}
			} else {
				if result.OverrideApplied {
					t.Errorf("OverrideApplied = true, want false (OverrideModel=%q)", result.OverrideModel)
				}
			}

			if result.ClassificationAge != "v1" {
				t.Errorf("ClassificationAge = %q, want %q", result.ClassificationAge, "v1")
			}
		})
	}
}

// cursorMessageParts reproduces a Cursor last-user-message as an array of
// content parts: a text part that carries the huge context block (recently
// viewed files, attached code selection, terminal output) plus the <user_query>
// wrapper holding the real laconic prompt. This is the shape that previously
// caused "fix all these" with a lint attachment to miss the automation
// classifier and stay on the expensive model.
func cursorMessageParts(attachMarker, prompt string) map[string]any {
	return map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "<system_reminder>context</system_reminder>\n<open_and_recently_viewed_files>\n</open_and_recently_viewed_files>\n" +
					"<code_selection path=\"x/m.js\">\n" + attachMarker + "\n</code_selection>\n" +
					"<timestamp>today</timestamp>\n<user_query>" + prompt + "</user_query>",
			},
		},
	}
}

func TestContentClassification_LintAttachDowngrades(t *testing.T) {
	tests := []struct {
		name         string
		attachMarker string
		prompt       string
		wantClass    RequestClass
	}{
		{
			name:         "fix all these + eslint no-undef attach → automation (flash)",
			attachMarker: "✖ 3 problems (3 errors)\n/path/file.mjs\n  36:19  error  'document' is not defined  no-undef\n\nexit status 1",
			prompt:       "fix all these",
			wantClass:    ClassAutomation,
		},
		{
			name:         "fix + astro-lint pre-commit output attach → automation (flash)",
			attachMarker: "lefthook v2.1.10 hook: pre-commit\n┃  astro-lint ❯\nexit status 1\n┃  prettier: code style issues",
			prompt:       "fix the formatting",
			wantClass:    ClassAutomation,
		},
		{
			name:         "real code edit without lint markers stays editing",
			attachMarker: "func f() *T { return nil }",
			prompt:       "Fix the nil pointer dereference in proxy.go",
			wantClass:    ClassEditing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{
				"model":    "deepseek-v4-pro",
				"messages": []any{cursorMessageParts(tt.attachMarker, tt.prompt)},
			}
			got := classifyRequest(body)
			if got != tt.wantClass {
				t.Errorf("classifyRequest() = %q, want %q", got, tt.wantClass)
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
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
		},
		{
			name: "summarization: capture conversation → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Capture the entire conversation into a dense, structured markdown summary file. Write it to disk."},
				},
			},
			wantClass:    ClassSimpleLookup, // summarization beats editing (despite "write")
			wantOverride: "glm-4.7",
		},
		{
			name: "summarization: summarize this chat → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Condense the following code review into key action items."},
				},
			},
			wantClass:    ClassSimpleLookup, // summarization beats editing
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
		},
		{
			name: "code search: long exploration → code search (exploration guard)",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": `I need to understand the webhook server architecture in this Go repo for adding a new endpoint. Please explore:

1. The main router in internal/gateway/router.go
2. The handler registration in internal/gateway/server.go
3. Find all existing webhook endpoints (look for "/webhook" patterns)
4. List the middleware chain applied to POST endpoints

This is a read-only exploration task — do not modify any files.`},
				},
			},
			wantClass:    ClassCodeSearch, // exploration guard blocks long-message heuristic; falls through to code search
			wantOverride: "glm-4.7",
		},
		{
			name: "code search + complex keyword → complex reasoning (explicit signals beat guard)",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Explore the authentication system and recommend a scalability strategy. Analyze trade-offs between JWT and sessions for a high-traffic API."},
				},
			},
			wantClass:    ClassComplexReasoning, // "trade-off" keyword triggers complex BEFORE code search
			wantOverride: "",
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
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
		},
		{
			name: "automation: pull request → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Create a PR with the current work"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
		},
		{
			name: "automation: gh pr → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "push the current branch and open a pr"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "glm-4.7",
		},
		{
			name: "automation: run script → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "run ci"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "glm-4.7",
		},
		{
			name: "automation: run a script → flash",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "deploy the latest build"},
				},
			},
			wantClass:    ClassAutomation,
			wantOverride: "glm-4.7",
		},
		{
			name: "editing+complex: refactor + pipeline → keep model (complex beats editing)",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "Refactor the sanitizer to use a design pattern with proper trade-off analysis"},
				},
			},
			wantClass:    ClassComplexReasoning, // "trade-off" keyword → complex beats refactor → editing
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
					map[string]any{"role": "user", "content": "We need a new architecture for the smart router that handles subagent detection, content-based classification, and model overrides. It should also support fallback tiers and automatic retry logic. What is the best way to design this system from a scalability perspective?"},
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
					map[string]any{"role": "user", "content": "How would you design the system architecture for multi-provider failover?"},
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
					map[string]any{"role": "user", "content": "Plan a comprehensive database migration strategy with rollback procedures"},
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
					map[string]any{"role": "user", "content": "Plan a scalable architecture for the notification system"},
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
					map[string]any{"role": "user", "content": "Design a release strategy with zero-downtime deployment"},
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
			wantClass:    ClassUnknown, // no recognizable pattern → keep model (conservative)
			wantOverride: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: true})
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
			name:      "summarization beats editing (contains 'write' and 'capture' but is text synthesis)",
			userMsg:   "Capture the conversation into a dense summary and write it to a file",
			wantClass: ClassSimpleLookup,
		},
		{
			name:      "refactor + pr → editing over automation (edited keyword wins for mixed)",
			userMsg:   "Refactor the sanitizer and open a pull request",
			wantClass: ClassEditing,
		},
		{
			name:      "pure automation: create PR → automation (no editing keywords)",
			userMsg:   "Create a PR for the current branch",
			wantClass: ClassAutomation,
		},
		{
			name:      "short planning message → complex reasoning (not simple lookup)",
			userMsg:   "Plan a new feature release",
			wantClass: ClassComplexReasoning,
		},
		{
			name:      "create a plan → complex reasoning (not simple lookup)",
			userMsg:   "Plan a rollout for the new API version",
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

func TestSubAgentRouter_ModelPreservedForMainAgent(t *testing.T) {
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
	r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: true})
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

func TestSubAgentRouter_UnknownProviderDefaultFallback(t *testing.T) {
	// Models not in the override map fall back to the default flash model
	// (glm-4.7) when classification triggers a downgrade.
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
			r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: true})
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

func TestSubAgentRouter_Glm53ToFlashViaDefault(t *testing.T) {
	// glm-5.3 resolves to a flash variant via the override map / universal
	// default (defaultFlashModel = glm-4.7).
	body := map[string]any{
		"model": "glm-5.3",
		"messages": []any{
			map[string]any{"role": "system", "content": longSystemPrompt},
			map[string]any{"role": "user", "content": "search for all test files"},
		},
	}
	r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")
	if !result.OverrideApplied {
		t.Errorf("glm-5.3 should be overridden (not in map → default), got class=%q", result.RequestClass)
	}
	if stringField(body, "model") != defaultFlashModel {
		t.Errorf("expected default %q, got %q", defaultFlashModel, stringField(body, "model"))
	}
}

func TestSubAgentRouter_SubagentOnFlashIsNoop(t *testing.T) {
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
	r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: true})
	result := r.ClassifyAndOverride(body, "req_test")
	if result.OverrideApplied {
		t.Errorf("flash → flash should be a no-op, but OverrideApplied=true (%q → %q)", result.OriginalModel, result.OverrideModel)
	}
	if stringField(body, "model") != "deepseek-v4-flash" {
		t.Errorf("flash model should stay flash, got %q", stringField(body, "model"))
	}
}

func TestSubAgentRouter_DisabledPreservesModelAndClassifies(t *testing.T) {
	// When disabled, classification still runs but no override.
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": longSystemPrompt},
			map[string]any{"role": "user", "content": "What is a goroutine?"},
		},
	}
	r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: false})
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
- gpt-4.1-turbo → Z.AI glm-5.3 (planning, cheaper than K3)
- gpt-4.1 → Z.AI glm-4.7 (cheap execution)

## Coding Standards
- Wrap errors with %w; return actionable messages at CLI boundaries.
- Never suppress linter warnings with //nolint comments.
- Never log API keys or raw Authorization headers.
- Prefer small packages with package comments and one-line Contracts.`

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
			expect: "", // the greedy summaryBlock regex matches to end of string
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
			wantOverride: "glm-4.7",
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
			wantOverride: "glm-4.7",
		},
		{
			name: "editing+complex wrapped in XML",
			body: map[string]any{
				"model": "deepseek-v4-pro",
				"messages": []any{
					map[string]any{"role": "system", "content": longSystemPrompt},
					map[string]any{"role": "user", "content": "<open_and_recently_viewed_files>\n- router.go\n</open_and_recently_viewed_files>\nDesign a scalable architecture for the classifyRequest pipeline with proper trade-off analysis"},
				},
			},
			wantClass:    ClassComplexReasoning, // "design" prefix + "trade-off" → complex
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
			r := NewSubAgentRouter(SubAgentRouterConfig{Enabled: true})
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
