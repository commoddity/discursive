package config

import "testing"

func TestNormalizeReasoningEffort(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		effort  string
		want    string
		wantErr bool
	}{
		{name: "k3 low", model: ModelKimiK3, effort: "low", want: "low"},
		{name: "k3 HIGH", model: ModelKimiK3, effort: "HIGH", want: "high"},
		{name: "k3 max", model: ModelKimiK3, effort: "max", want: "max"},
		{name: "k3 medium invalid", model: ModelKimiK3, effort: "medium", wantErr: true},
		{name: "k3 off invalid", model: ModelKimiK3, effort: "off", wantErr: true},
		// Kimi K2.7 Code always thinks — no effort selector; any effort is invalid.
		{name: "k27 off invalid", model: ModelKimiK27, effort: "off", wantErr: true},
		{name: "k27 on invalid", model: ModelKimiK27, effort: "on", wantErr: true},
		{name: "k27 low invalid", model: ModelKimiK27, effort: "low", wantErr: true},
		{name: "ds off", model: ModelDeepSeekV4Pro, effort: "off", want: "off"},
		{name: "ds high", model: ModelDeepSeekV4Pro, effort: "high", want: "high"},
		{name: "ds max", model: ModelDeepSeekV4Flash, effort: "max", want: "max"},
		{name: "ds low maps to high", model: ModelDeepSeekV4Flash, effort: "low", want: "high"},
		{name: "ds medium maps to high", model: ModelDeepSeekV4Pro, effort: "medium", want: "high"},
		{name: "ds xhigh maps to max", model: ModelDeepSeekV4Pro, effort: "xhigh", want: "max"},
		{name: "ds garbage", model: ModelDeepSeekV4Pro, effort: "turbo", wantErr: true},
		{name: "zai off maps to low", model: ModelZaiGLM53, effort: "off", want: "low"},
		{name: "zai none maps to low", model: ModelZaiGLM53, effort: "none", want: "low"},
		{name: "zai minimal maps to low", model: ModelZaiGLM53, effort: "minimal", want: "low"},
		{name: "zai low", model: ModelZaiGLM53, effort: "low", want: "low"},
		{name: "zai HIGH", model: ModelZaiGLM53, effort: "HIGH", want: "high"},
		{name: "zai max", model: ModelZaiGLM53, effort: "max", want: "max"},
		{name: "zai medium maps to high", model: ModelZaiGLM53, effort: "medium", want: "high"},
		{name: "zai xhigh maps to max", model: ModelZaiGLM53, effort: "xhigh", want: "max"},
		{name: "zai garbage", model: ModelZaiGLM53, effort: "turbo", wantErr: true},
		{name: "unknown model", model: "thaura", effort: "low", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeReasoningEffort(tt.model, tt.effort)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeReasoningEffortMapDefaults(t *testing.T) {
	got := NormalizeReasoningEffortMap(nil)
	if got[ModelKimiK3] != "low" {
		t.Fatalf("k3 default: %q", got[ModelKimiK3])
	}
	if got[ModelDeepSeekV4Pro] != EffortOff {
		t.Fatalf("pro default: %q", got[ModelDeepSeekV4Pro])
	}
	if got[ModelZaiGLM53] != "low" {
		t.Fatalf("zai glm-5.3 default: %q", got[ModelZaiGLM53])
	}
	got = NormalizeReasoningEffortMap(map[string]string{
		ModelKimiK3:          "max",
		ModelDeepSeekV4Flash: "medium", // legacy alias → high
		ModelZaiGLM53:        "high",
		"thaura":             "low",
	})
	if got[ModelKimiK3] != "max" {
		t.Fatalf("k3: %q", got[ModelKimiK3])
	}
	if got[ModelDeepSeekV4Flash] != "high" {
		t.Fatalf("flash medium→high: %q", got[ModelDeepSeekV4Flash])
	}
	if got[ModelZaiGLM53] != "high" {
		t.Fatalf("zai glm-5.3: %q", got[ModelZaiGLM53])
	}
	if _, ok := got["thaura"]; ok {
		t.Fatal("thaura should be dropped")
	}
}
