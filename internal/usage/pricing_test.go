package usage

import (
	"math"
	"testing"

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
			name:     "kimi_k26_code",
			provider: config.ProviderMoonshot,
			model:    "kimi-k2.6",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EstimateUSD(tt.provider, tt.model, tt.tokens)
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
