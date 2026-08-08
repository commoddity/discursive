// Package usageui — snapshot controller
//
// Periodically fetches provider balances (Moonshot, DeepSeek, Z.AI) and stores
// balance snapshots with bases "sample", "day", "week", and "month".  Thaura
// does not expose a balance API and is skipped.
//
// Confirmed spend per period is computed by the /api/balance-spend handler in
// balance.go using the snapshots stored here.
package usageui

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

const snapshotInterval = 15 * time.Minute

// StartSnapshots begins periodic balance snapshot capture on the server.  The
// key source must be configured before this is called.  The parent context
// controls lifetime; StartSnapshots derives its own child context so the caller
// does not need to track the cancel function.
func (s *Server) StartSnapshots(ctx context.Context) {
	if s == nil || s.snapCtrl != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	_ = cancel // Start derives and owns its own cancellable child context.
	ctrl := NewSnapshotController(s.store, s.httpClient, s.keySource, slog.Default())
	ctrl.Start(ctx)
	s.snapCtrl = ctrl
}

// SnapshotController periodically captures provider balances and persists them.
type SnapshotController struct {
	store  *usage.Store
	client *http.Client
	ks     KeySource
	log    *slog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSnapshotController creates a controller with the given dependencies.
func NewSnapshotController(store *usage.Store, client *http.Client, ks KeySource, logger *slog.Logger) *SnapshotController {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SnapshotController{
		store:  store,
		client: client,
		ks:     ks,
		log:    logger,
	}
}

// Start begins the snapshot loop.  Call only once per controller.
func (c *SnapshotController) Start(ctx context.Context) {
	if c == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.log.Info("snapshot controller started", "interval", snapshotInterval)

	c.wg.Add(1)
	go c.loop(ctx)
}

// Stop gracefully terminates the loop.  Blocks until the goroutine exits.
func (c *SnapshotController) Stop() {
	if c == nil || c.cancel == nil {
		return
	}
	c.cancel()
	c.wg.Wait()
	c.log.Info("snapshot controller stopped")
}

func (c *SnapshotController) loop(ctx context.Context) {
	defer c.wg.Done()

	c.capture(ctx)

	ticker := time.NewTicker(snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.capture(ctx)
		}
	}
}

// capture fetches all provider balances and stores one snapshot row per
// provider × basis (sample, day, week, month).
func (c *SnapshotController) capture(ctx context.Context) {
	now := time.Now().UTC()
	log := c.log.With("ts", now.Format(time.RFC3339))

	var moonUSD, dsUSD float64
	var moonOK, dsOK bool
	var zaiCredits float64
	var zaiOK bool

	// Fetch all three in parallel for speed.
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		bal := fetchMoonshotBalance(c.client, c.ks.Moonshot)
		if bal.AvailableUSD != nil {
			moonUSD = *bal.AvailableUSD
			moonOK = true
		} else {
			log.Warn("moonshot balance no USD", "err", bal.Error)
		}
	}()

	go func() {
		defer wg.Done()
		bal := fetchDeepSeekBalance(c.client, c.ks.DeepSeek)
		if bal.AvailableUSD != nil {
			dsUSD = *bal.AvailableUSD
			dsOK = true
		} else {
			log.Warn("deepseek balance no USD", "err", bal.Error)
		}
	}()

	go func() {
		defer wg.Done()
		bal := fetchZaiBalance(c.client, c.ks.Zai)
		if bal.Amount != nil {
			zaiCredits = *bal.Amount
			zaiOK = true
		} else {
			log.Warn("zai balance no amount", "err", bal.Error)
		}
	}()

	wg.Wait()

	now = time.Now().UTC() // grab a fresh timestamp after the fetches

	if moonOK {
		for _, b := range basesForTime(now) {
			c.storeSnapshot(config.ProviderMoonshot, b, now, moonUSD, "USD", moonUSD)
		}
	}
	if dsOK {
		for _, b := range basesForTime(now) {
			c.storeSnapshot(config.ProviderDeepSeek, b, now, dsUSD, "USD", dsUSD)
		}
	}
	if zaiOK {
		for _, b := range basesForTime(now) {
			c.storeSnapshot(config.ProviderZai, b, now, zaiCredits, "credits", 0)
		}
	}
}

func (c *SnapshotController) storeSnapshot(prov config.Provider, b basisEntry, capturedAt time.Time, amount float64, currency string, usd float64) {
	snap := usage.BalanceSnapshot{
		Provider:    prov,
		Basis:       b.basis,
		PeriodStart: b.periodStart,
		CapturedAt:  capturedAt,
		Amount:      amount,
		Currency:    currency,
		USDAmount:   usd,
	}
	if err := c.store.InsertBalanceSnapshot(snap); err != nil {
		c.log.Warn("insert snapshot failed",
			"provider", prov, "basis", b.basis, "err", err)
	}
}

// ---- period helpers ----------------------------------------------------------

type basisEntry struct {
	basis       string
	periodStart time.Time
}

func basesForTime(t time.Time) []basisEntry {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

	// Monday is the start of the iso week.
	weekday := t.Weekday()
	if weekday == time.Sunday {
		weekday = 6
	} else {
		weekday = weekday - 1
	}
	week := day.Add(-time.Duration(weekday) * 24 * time.Hour)

	month := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)

	return []basisEntry{
		{basis: "sample", periodStart: t.Truncate(time.Minute)},
		{basis: "day", periodStart: day},
		{basis: "week", periodStart: week},
		{basis: "month", periodStart: month},
	}
}
