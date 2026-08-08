package usage

import (
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestAllocateByModel(t *testing.T) {
	t.Run("proportional split", func(t *testing.T) {
		models := []ModelWeight{
			{Model: "kimi-k3", Provider: config.ProviderMoonshot, Weight: 4.0},
			{Model: "kimi-k2.7-code", Provider: config.ProviderMoonshot, Weight: 1.0},
		}
		allocated, unalloc := AllocateByModel(10.0, models)
		// 4/5 * 10 = 8.0, 1/5 * 10 = 2.0
		if len(allocated) != 2 {
			t.Fatalf("got %d, want 2", len(allocated))
		}
		if allocated[0].Weight != 8.0 {
			t.Fatalf("kimi-k3 got %v, want 8.0", allocated[0].Weight)
		}
		if allocated[1].Weight != 2.0 {
			t.Fatalf("kimi-k2.7-code got %v, want 2.0", allocated[1].Weight)
		}
		if unalloc != 0 {
			t.Fatalf("unallocated: %v, want 0", unalloc)
		}
	})

	t.Run("zero confirmed", func(t *testing.T) {
		models := []ModelWeight{
			{Model: "a", Provider: config.ProviderMoonshot, Weight: 5.0},
		}
		allocated, unalloc := AllocateByModel(0, models)
		if allocated[0].Weight != 5.0 {
			t.Fatalf("got %v, want 5.0 (unmodified)", allocated[0].Weight)
		}
		if unalloc != 0 {
			t.Fatalf("unallocated: %v, want 0", unalloc)
		}
	})

	t.Run("zero total weight", func(t *testing.T) {
		models := []ModelWeight{
			{Model: "a", Provider: config.ProviderMoonshot, Weight: 0},
			{Model: "b", Provider: config.ProviderMoonshot, Weight: 0},
		}
		allocated, unalloc := AllocateByModel(10.0, models)
		if len(allocated) != 2 {
			t.Fatalf("got %d models, want 2", len(allocated))
		}
		if unalloc != 10.0 {
			t.Fatalf("unallocated: %v, want 10.0", unalloc)
		}
	})

	t.Run("supports deepseek", func(t *testing.T) {
		models := []ModelWeight{
			{Model: "deepseek-v4-flash", Provider: config.ProviderDeepSeek, Weight: 1.0},
			{Model: "deepseek-v4-pro", Provider: config.ProviderDeepSeek, Weight: 1.0},
		}
		allocated, _ := AllocateByModel(6.0, models)
		if allocated[0].Weight != 3.0 || allocated[1].Weight != 3.0 {
			t.Fatalf("got %v/%v, want 3.0/3.0", allocated[0].Weight, allocated[1].Weight)
		}
	})
}
