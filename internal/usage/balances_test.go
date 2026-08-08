package usage

import (
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestInsertAndConfirmedSpend(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	day := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	nextDay := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	snaps := []BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: day, CapturedAt: now.Add(0 * time.Minute), Amount: 50.0, Currency: "USD", USDAmount: 50.0},
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: day, CapturedAt: now.Add(30 * time.Minute), Amount: 49.0, Currency: "USD", USDAmount: 49.0},
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: day, CapturedAt: now.Add(5 * time.Hour), Amount: 47.5, Currency: "USD", USDAmount: 47.5},
		{Provider: config.ProviderDeepSeek, Basis: "day", PeriodStart: day, CapturedAt: now.Add(0 * time.Minute), Amount: 20.0, Currency: "USD", USDAmount: 20.0},
		{Provider: config.ProviderDeepSeek, Basis: "day", PeriodStart: day, CapturedAt: now.Add(1 * time.Hour), Amount: 18.5, Currency: "USD", USDAmount: 18.5},
	}
	for _, s := range snaps {
		if err := store.InsertBalanceSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("confirmed spend moonshot day", func(t *testing.T) {
		spend, err := store.ConfirmedSpend(config.ProviderMoonshot, "day", now.Add(-1*time.Hour), nextDay.Add(1*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if spend != 2.5 {
			t.Fatalf("got %v want 2.5", spend)
		}
	})

	t.Run("confirmed spend deepseek day", func(t *testing.T) {
		spend, err := store.ConfirmedSpend(config.ProviderDeepSeek, "day", now.Add(-1*time.Hour), nextDay.Add(1*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if spend != 1.5 {
			t.Fatalf("got %v want 1.5", spend)
		}
	})

	t.Run("no snapshots in range", func(t *testing.T) {
		spend, err := store.ConfirmedSpend(config.ProviderMoonshot, "day", time.Now().AddDate(1, 0, 0), time.Now().AddDate(1, 0, 1))
		if err != nil {
			t.Fatal(err)
		}
		if spend != 0 {
			t.Fatalf("got %v want 0", spend)
		}
	})

	t.Run("single snapshot returns zero", func(t *testing.T) {
		root2 := t.TempDir()
		s2, err := NewStore(root2)
		if err != nil {
			t.Fatal(err)
		}
		if err := s2.InsertBalanceSnapshot(BalanceSnapshot{
			Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: day,
			CapturedAt: now, Amount: 10, Currency: "USD", USDAmount: 10,
		}); err != nil {
			t.Fatal(err)
		}
		spend, err := s2.ConfirmedSpend(config.ProviderMoonshot, "day", now.Add(-1*time.Hour), now.Add(1*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if spend != 0 {
			t.Fatalf("got %v want 0 (need >=2 snapshots)", spend)
		}
	})
}

func TestDeleteBalanceSnapshotsBefore(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	recent := now.Add(-30 * 24 * time.Hour)

	if err := store.InsertBalanceSnapshot(BalanceSnapshot{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: old, CapturedAt: old, Amount: 10, USDAmount: 10}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertBalanceSnapshot(BalanceSnapshot{Provider: config.ProviderDeepSeek, Basis: "day", PeriodStart: recent, CapturedAt: recent, Amount: 20, USDAmount: 20}); err != nil {
		t.Fatal(err)
	}

	n, err := store.DeleteBalanceSnapshotsBefore(now.Add(-60 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d, want 1", n)
	}

	latest, err := store.LatestBalanceSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 1 {
		t.Fatalf("remaining: %d, want 1", len(latest))
	}
}

func TestLatestBalanceSnapshots(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	if err := store.InsertBalanceSnapshot(BalanceSnapshot{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: now, CapturedAt: now, Amount: 50, USDAmount: 50}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: now, CapturedAt: now.Add(1 * time.Hour), Amount: 49, USDAmount: 49},
		{Provider: config.ProviderMoonshot, Basis: "week", PeriodStart: now, CapturedAt: now, Amount: 52, USDAmount: 52},
		{Provider: config.ProviderDeepSeek, Basis: "day", PeriodStart: now, CapturedAt: now, Amount: 20, USDAmount: 20},
	} {
		if err := store.InsertBalanceSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}

	snaps, err := store.LatestBalanceSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 3 {
		t.Fatalf("got %d latest snapshots, want 3", len(snaps))
	}
}

func TestBalanceSnapshotsForProvider(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		if err := store.InsertBalanceSnapshot(BalanceSnapshot{
			Provider: config.ProviderMoonshot, Basis: "day",
			PeriodStart: now, CapturedAt: now.Add(time.Duration(i) * time.Hour),
			Amount: float64(50 - i), USDAmount: float64(50 - i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	snaps, err := store.BalanceSnapshotsForProvider(config.ProviderMoonshot, "day", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 5 {
		t.Fatalf("got %d snapshots, want 5", len(snaps))
	}
}
