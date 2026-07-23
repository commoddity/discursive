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
		{name: "k26 off", model: ModelKimiK26, effort: "off", want: "off"},
		{name: "k26 on", model: ModelKimiK26, effort: "on", want: "on"},
		{name: "k26 high invalid", model: ModelKimiK26, effort: "high", wantErr: true},
		{name: "ds off", model: ModelDeepSeekV4Pro, effort: "off", want: "off"},
		{name: "ds high", model: ModelDeepSeekV4Pro, effort: "high", want: "high"},
		{name: "ds max", model: ModelDeepSeekV4Flash, effort: "max", want: "max"},
		{name: "ds low maps to high", model: ModelDeepSeekV4Flash, effort: "low", want: "high"},
		{name: "ds medium maps to high", model: ModelDeepSeekV4Pro, effort: "medium", want: "high"},
		{name: "ds xhigh maps to max", model: ModelDeepSeekV4Pro, effort: "xhigh", want: "max"},
		{name: "ds garbage", model: ModelDeepSeekV4Pro, effort: "turbo", wantErr: true},
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
	got = NormalizeReasoningEffortMap(map[string]string{
		ModelKimiK3:          "max",
		ModelDeepSeekV4Flash: "medium", // legacy alias → high
		"thaura":             "low",
	})
	if got[ModelKimiK3] != "max" {
		t.Fatalf("k3: %q", got[ModelKimiK3])
	}
	if got[ModelDeepSeekV4Flash] != "high" {
		t.Fatalf("flash medium→high: %q", got[ModelDeepSeekV4Flash])
	}
	if _, ok := got["thaura"]; ok {
		t.Fatal("thaura should be dropped")
	}
}
