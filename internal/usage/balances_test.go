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

func TestAllPeriodBases(t *testing.T) {
	bases := AllPeriodBases()
	if len(bases) != 3 {
		t.Fatalf("got %d bases, want 3", len(bases))
	}
	seen := map[string]bool{}
	for _, b := range bases {
		seen[b] = true
		if b != "day" && b != "week" && b != "month" {
			t.Errorf("unexpected basis: %s", b)
		}
	}
	for _, want := range []string{"day", "week", "month"} {
		if !seen[want] {
			t.Errorf("missing basis: %s", want)
		}
	}
	// "sample" must not be in AllPeriodBases.
	if seen["sample"] {
		t.Error("sample should not be in AllPeriodBases")
	}
}

func TestInsertBalanceSnapshot_EmptyProvider(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Inserting with empty provider should still work (no constraint violation).
	err = store.InsertBalanceSnapshot(BalanceSnapshot{
		Provider:   "",
		Basis:      "day",
		CapturedAt: time.Now(),
		Amount:     10,
	})
	if err != nil {
		t.Fatalf("empty provider insert failed: %v", err)
	}
}

func TestInsertBalanceSnapshot_AllFieldsPopulated(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = store.InsertBalanceSnapshot(BalanceSnapshot{
		Provider:    config.ProviderMoonshot,
		Basis:       "month",
		PeriodStart: now,
		CapturedAt:  now,
		Amount:      42.42,
		Currency:    "USD",
		USDAmount:   42.42,
	})
	if err != nil {
		t.Fatalf("full insert failed: %v", err)
	}

	snaps, err := store.LatestBalanceSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range snaps {
		if s.Provider == config.ProviderMoonshot && s.Basis == "month" {
			found = true
			if s.Amount != 42.42 {
				t.Errorf("amount: got %v, want 42.42", s.Amount)
			}
		}
	}
	if !found {
		t.Error("inserted snapshot not found via LatestBalanceSnapshots")
	}
}

func TestConfirmedSpend_ExactBoundaries(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	snaps := []BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day",
			PeriodStart: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
			CapturedAt:  now.Add(-2 * time.Hour), Amount: 100, Currency: "USD", USDAmount: 100},
		{Provider: config.ProviderMoonshot, Basis: "day",
			PeriodStart: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
			CapturedAt:  now, Amount: 95, Currency: "USD", USDAmount: 95},
	}
	for _, s := range snaps {
		if err := store.InsertBalanceSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}

	// Range that exactly matches snapshot timestamps.
	after := now.Add(-2 * time.Hour)
	before := now
	spend, err := store.ConfirmedSpend(config.ProviderMoonshot, "day", after, before)
	if err != nil {
		t.Fatal(err)
	}
	if spend != 5.0 {
		t.Fatalf("got %v, want 5.0", spend)
	}
}

func TestConfirmedSpend_NoOverlap(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if err := store.InsertBalanceSnapshot(BalanceSnapshot{
		Provider: config.ProviderMoonshot, Basis: "day",
		PeriodStart: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		CapturedAt:  now, Amount: 50, Currency: "USD", USDAmount: 50,
	}); err != nil {
		t.Fatal(err)
	}

	// Query range completely outside the snapshot.
	spend, err := store.ConfirmedSpend(config.ProviderMoonshot, "day",
		now.Add(1*time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if spend != 0 {
		t.Fatalf("got %v, want 0 (no snapshots in range)", spend)
	}
}

func TestDeleteBalanceSnapshotsBefore_AllDeleted(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := store.InsertBalanceSnapshot(BalanceSnapshot{
			Provider: config.ProviderMoonshot, Basis: "day",
			PeriodStart: now, CapturedAt: now.Add(-time.Duration(i) * 24 * time.Hour),
			Amount: 10, Currency: "USD", USDAmount: 10,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Delete everything (cutoff after all timestamps).
	n, err := store.DeleteBalanceSnapshotsBefore(now.Add(1 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("deleted %d, want 3", n)
	}

	snaps, err := store.LatestBalanceSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("remaining: %d, want 0", len(snaps))
	}
}

func TestConfirmedSpend_IdenticalBalances(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)

	snaps := []BalanceSnapshot{
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now.Add(-1 * time.Hour), Amount: 50, Currency: "USD", USDAmount: 50},
		{Provider: config.ProviderMoonshot, Basis: "day", PeriodStart: dayStart,
			CapturedAt: now, Amount: 50, Currency: "USD", USDAmount: 50},
	}
	for _, s := range snaps {
		if err := store.InsertBalanceSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}
	spend, err := store.ConfirmedSpend(config.ProviderMoonshot, "day",
		now.Add(-2*time.Hour), now.Add(1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if spend != 0 {
		t.Fatalf("got %v, want 0 (identical balances)", spend)
	}
}

func TestPeriodCovered(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	monthStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	// No snapshots at all.
	t.Run("no snapshots", func(t *testing.T) {
		covered, err := store.PeriodCovered(config.ProviderMoonshot, "month", monthStart, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if covered {
			t.Fatal("expected false with no snapshots")
		}
	})

	// Snapshot captured 30 minutes after period start — within tolerance.
	t.Run("within tolerance", func(t *testing.T) {
		if err := store.InsertBalanceSnapshot(BalanceSnapshot{
			Provider: config.ProviderMoonshot, Basis: "month",
			PeriodStart: monthStart,
			CapturedAt:  monthStart.Add(30 * time.Minute),
			Amount:      100, Currency: "USD", USDAmount: 100,
		}); err != nil {
			t.Fatal(err)
		}
		covered, err := store.PeriodCovered(config.ProviderMoonshot, "month", monthStart, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if !covered {
			t.Fatal("expected true within tolerance")
		}
	})

	// Snapshot captured 2 hours after period start — outside tolerance.
	t.Run("outside tolerance", func(t *testing.T) {
		store2, _ := NewStore(t.TempDir())
		_ = store2.InsertBalanceSnapshot(BalanceSnapshot{
			Provider: config.ProviderMoonshot, Basis: "month",
			PeriodStart: monthStart,
			CapturedAt:  monthStart.Add(2 * time.Hour),
			Amount:      100, Currency: "USD", USDAmount: 100,
		})
		covered, err := store2.PeriodCovered(config.ProviderMoonshot, "month", monthStart, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if covered {
			t.Fatal("expected false outside tolerance")
		}
	})

	// Snapshot exists but before the period start.
	t.Run("before period start", func(t *testing.T) {
		store3, _ := NewStore(t.TempDir())
		// Insert a snapshot from the previous month.
		_ = store3.InsertBalanceSnapshot(BalanceSnapshot{
			Provider: config.ProviderMoonshot, Basis: "month",
			PeriodStart: monthStart,
			CapturedAt:  monthStart.Add(-1 * time.Hour), // 1 hour before Aug 1
			Amount:      100, Currency: "USD", USDAmount: 100,
		})
		covered, err := store3.PeriodCovered(config.ProviderMoonshot, "month", monthStart, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if covered {
			t.Fatal("expected false — snapshot is before period start, filtered out by query")
		}
	})
}
