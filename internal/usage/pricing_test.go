package usage

import (
	"math"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestEstimateUSD(t *testing.T) {
	const eps = 1e-9
	tests := []struct {
		name     string
		provider config.Provider
		model    string
		tokens   UsageTokens
		want     float64
		wantErr  bool
	}{
		{
			name:     "kimi_k3_all_input",
			provider: config.ProviderMoonshot,
			model:    "kimi-k3",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 0},
			want:     3.00,
		},
		{
			name:     "kimi_k3_cache_hit_only",
			provider: config.ProviderMoonshot,
			model:    "kimi-k3",
			tokens:   UsageTokens{CacheHitTokens: 1_000_000},
			want:     0.30,
		},
		{
			name:     "kimi_k3_partial_cache_coverage",
			provider: config.ProviderMoonshot,
			model:    "kimi-k3",
			tokens: UsageTokens{
				PromptTokens:     191_241,
				CacheHitTokens:   94_208,
				CompletionTokens: 232,
			},
			// hit: 94208 × 0.30/M, miss: (191241 - 94208) × 3.00/M, output: 232 × 15.00/M
			want: perMillion(94208, 0.30) + perMillion(191241-94208, 3.00) + perMillion(232, 15.00),
		},
		{
			name:     "kimi_k3_mixed",
			provider: config.ProviderMoonshot,
			model:    "kimi-k3",
			tokens: UsageTokens{
				CacheHitTokens:   500_000,
				CacheMissTokens:  500_000,
				CompletionTokens: 100_000,
			},
			want: perMillion(500_000, 0.30) + perMillion(500_000, 3.00) + perMillion(100_000, 15.00),
		},
		{
			name:     "kimi_k27_code",
			provider: config.ProviderMoonshot,
			model:    "kimi-k2.7-code",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			want:     0.95 + 4.00,
		},
		{
			name:     "deepseek_flash_miss",
			provider: config.ProviderDeepSeek,
			model:    "deepseek-v4-flash",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			want:     0.14 + 0.28,
		},
		{
			name:     "deepseek_pro_cache_hit",
			provider: config.ProviderDeepSeek,
			model:    "deepseek-v4-pro",
			tokens:   UsageTokens{CacheHitTokens: 1_000_000},
			want:     0.003625,
		},
		{
			name:     "deepseek_flash_split",
			provider: config.ProviderDeepSeek,
			model:    "deepseek-v4-flash",
			tokens: UsageTokens{
				CacheHitTokens:   1_000_000,
				CacheMissTokens:  2_000_000,
				CompletionTokens: 500_000,
			},
			want: perMillion(1_000_000, 0.0028) + perMillion(2_000_000, 0.14) + perMillion(500_000, 0.28),
		},
		{
			name:     "unknown_moonshot_model",
			provider: config.ProviderMoonshot,
			model:    "kimi-unknown",
			tokens:   UsageTokens{PromptTokens: 100},
			wantErr:  true,
		},
		{
			name:     "unknown_deepseek_model",
			provider: config.ProviderDeepSeek,
			model:    "deepseek-v9",
			tokens:   UsageTokens{PromptTokens: 100},
			wantErr:  true,
		},
		{
			name:     "thaura_input_only",
			provider: config.ProviderThaura,
			model:    "thaura",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 0},
			want:     0.50,
		},
		{
			name:     "thaura_mixed",
			provider: config.ProviderThaura,
			model:    "thaura",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			want:     0.50 + 2.00,
		},
		{
			name:     "unknown_thaura_model",
			provider: config.ProviderThaura,
			model:    "thaura-unknown",
			tokens:   UsageTokens{PromptTokens: 100},
			wantErr:  true,
		},
		{
			name:     "zai_glm53_mixed",
			provider: config.ProviderZai,
			model:    "glm-5.3",
			tokens: UsageTokens{
				CacheHitTokens:   1_000_000,
				CacheMissTokens:  500_000,
				CompletionTokens: 100_000,
			},
			want: perMillion(1_000_000, 0.26) + perMillion(500_000, 1.40) + perMillion(100_000, 4.40),
		},
		{
			name:     "zai_glm53_cache_hit_only",
			provider: config.ProviderZai,
			model:    "glm-5.3",
			tokens:   UsageTokens{CacheHitTokens: 1_000_000},
			want:     0.26,
		},
		{
			name:     "zai_glm53_input_only",
			provider: config.ProviderZai,
			model:    "glm-5.3",
			tokens:   UsageTokens{PromptTokens: 1_000_000},
			want:     1.40,
		},
		{
			name:     "zai_glm47_input_output",
			provider: config.ProviderZai,
			model:    "glm-4.7",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			want:     0.46 + 1.60,
		},
		{
			name:     "unknown_zai_model",
			provider: config.ProviderZai,
			model:    "glm-unknown",
			tokens:   UsageTokens{PromptTokens: 100},
			wantErr:  true,
		},
		{
			name:     "zai_glm46v_input_only",
			provider: config.ProviderZai,
			model:    "glm-4.6v",
			tokens:   UsageTokens{PromptTokens: 1_000_000},
			want:     0.12,
		},
		{
			name:     "zai_glm46v_cache_hit_only",
			provider: config.ProviderZai,
			model:    "glm-4.6v",
			tokens:   UsageTokens{CacheHitTokens: 1_000_000},
			want:     0.03,
		},
		{
			name:     "zai_glm46v_mixed",
			provider: config.ProviderZai,
			model:    "glm-4.6v",
			tokens: UsageTokens{
				CacheHitTokens:   1_000_000,
				CacheMissTokens:  500_000,
				CompletionTokens: 100_000,
			},
			want: perMillion(1_000_000, 0.03) + perMillion(500_000, 0.12) + perMillion(100_000, 0.27),
		},
		{
			name:     "openrouter_flash_input_output",
			provider: config.ProviderOpenRouter,
			model:    "deepseek/deepseek-v4-flash-0731",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			want:     0.065 + 0.14,
		},
		{
			name:     "openrouter_flash_cache_hit",
			provider: config.ProviderOpenRouter,
			model:    "deepseek/deepseek-v4-flash-0731",
			tokens:   UsageTokens{CacheHitTokens: 1_000_000},
			want:     0.014,
		},
		{
			name:     "openrouter_pro_input_output",
			provider: config.ProviderOpenRouter,
			model:    "deepseek/deepseek-v4-pro-0813",
			tokens:   UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			want:     1.188 + 3.564,
		},
		{
			name:     "openrouter_pro_cache_hit",
			provider: config.ProviderOpenRouter,
			model:    "deepseek/deepseek-v4-pro-0813",
			tokens:   UsageTokens{CacheHitTokens: 1_000_000},
			want:     0.0396,
		},
		{
			name:     "unknown_openrouter_model",
			provider: config.ProviderOpenRouter,
			model:    "openai/gpt-99",
			tokens:   UsageTokens{PromptTokens: 100},
			wantErr:  true,
		},
	}

	// Legacy flat card: all cases price at a pre-cutover instant.
	at := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EstimateUSDAt(tt.provider, tt.model, tt.tokens, at)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-tt.want) > eps {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

// TestEstimateUSDDepSeekPeakOffPeak covers the post-cutover rate card:
// off-peak base rates, peak hours at 2x, and window boundaries.
func TestEstimateUSDDepSeekPeakOffPeak(t *testing.T) {
	const eps = 1e-9
	post := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) // off-peak
	flash := UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	pro := UsageTokens{CacheHitTokens: 1_000_000}

	tests := []struct {
		name  string
		model string
		at    time.Time
		toks  UsageTokens
		want  float64
	}{
		{
			name:  "flash_off_peak",
			model: "deepseek-v4-flash",
			at:    post,
			toks:  flash,
			want:  0.22 + 0.66,
		},
		{
			name:  "flash_peak_hour_1",
			model: "deepseek-v4-flash",
			at:    time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
			toks:  flash,
			want:  0.44 + 1.32,
		},
		{
			name:  "flash_peak_hour_9",
			model: "deepseek-v4-flash",
			at:    time.Date(2026, 8, 17, 9, 59, 59, 0, time.UTC),
			toks:  flash,
			want:  0.44 + 1.32,
		},
		{
			name:  "flash_off_peak_hour_4_start",
			model: "deepseek-v4-flash",
			at:    time.Date(2026, 8, 17, 4, 0, 0, 0, time.UTC),
			toks:  flash,
			want:  0.22 + 0.66,
		},
		{
			name:  "flash_off_peak_hour_5_lunch_gap",
			model: "deepseek-v4-flash",
			at:    time.Date(2026, 8, 17, 5, 0, 0, 0, time.UTC),
			toks:  flash,
			want:  0.22 + 0.66,
		},
		{
			name:  "flash_off_peak_hour_10_start",
			model: "deepseek-v4-flash",
			at:    time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC),
			toks:  flash,
			want:  0.22 + 0.66,
		},
		{
			name:  "pro_off_peak_cache_hit",
			model: "deepseek-v4-pro",
			at:    post,
			toks:  pro,
			want:  0.022,
		},
		{
			name:  "pro_peak_cache_hit",
			model: "deepseek-v4-pro",
			at:    time.Date(2026, 8, 17, 6, 30, 0, 0, time.UTC),
			toks:  pro,
			want:  0.044,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EstimateUSDAt(config.ProviderDeepSeek, tt.model, tt.toks, tt.at)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-tt.want) > eps {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

// TestDeepSeekCutoverBoundary pins the exact switchover instant.
func TestDeepSeekCutoverBoundary(t *testing.T) {
	const eps = 1e-9
	toks := UsageTokens{PromptTokens: 1_000_000}
	before := time.Date(2026, 8, 16, 15, 59, 59, 0, time.UTC)
	at := time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC)

	gotLegacy, err := EstimateUSDAt(config.ProviderDeepSeek, "deepseek-v4-flash", toks, before)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(gotLegacy-0.14) > eps {
		t.Fatalf("pre-cutover flash input = %v, want 0.14 (legacy flat card)", gotLegacy)
	}

	gotNew, err := EstimateUSDAt(config.ProviderDeepSeek, "deepseek-v4-flash", toks, at)
	if err != nil {
		t.Fatal(err)
	}
	// 16:00 UTC is off-peak (not in 01–04 / 06–10 windows).
	if math.Abs(gotNew-0.22) > eps {
		t.Fatalf("cutover instant flash input = %v, want 0.22 (new off-peak card)", gotNew)
	}
}

func TestEstimateUSDNeverUsesCursorComparison(t *testing.T) {
	// Cursor reference rates are much lower input — if used, kimi-k3 1M prompt would be $0.50 not $3.
	got, err := EstimateUSD(config.ProviderMoonshot, "kimi-k3", UsageTokens{PromptTokens: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	refIn, _, _ := CursorComparisonReference()
	if math.Abs(got-refIn) < 0.01 {
		t.Fatal("EstimateUSD appears to use Cursor comparison rates")
	}
	if got != 3.00 {
		t.Fatalf("got %v want 3.00", got)
	}
}

func TestCursorComparisonReferencePresent(t *testing.T) {
	in, cache, out := CursorComparisonReference()
	if in <= 0 || cache <= 0 || out <= 0 {
		t.Fatalf("cursor reference constants unset: %v %v %v", in, cache, out)
	}
}
