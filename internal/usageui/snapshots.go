// Package usageui — snapshot controller
//
// Periodically fetches provider balances (Moonshot, DeepSeek, Z.AI) and stores
// 'sample' balance snapshots.  Thaura does not expose a balance API and is
// skipped. A staleness watchdog backstops the periodic ticker so a provider
// never goes longer than snapshotInterval without a fresh sample.
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

	"golang.org/x/sync/singleflight"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

const (
	snapshotInterval  = 15 * time.Minute
	stalenessInterval = 30 * time.Second
)

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
	sfg    singleflight.Group // dedupes overlapping captures (ticker + watchdog)
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
	c.wg.Add(1)
	go c.stalenessWatchdog(ctx)
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

// capture fetches all provider balances and stores one 'sample' snapshot row per
// provider. It is wrapped in a singleflight so overlapping triggers (periodic
// ticker + staleness watchdog) collapse into a single fetch.
func (c *SnapshotController) capture(ctx context.Context) {
	_, _, _ = c.sfg.Do("capture", func() (any, error) {
		c.captureOnce(ctx)
		return nil, nil
	})
}

func (c *SnapshotController) captureOnce(ctx context.Context) {
	now := time.Now().UTC()
	log := c.log.With("ts", now.Format(time.RFC3339))

	var moonUSD, moonTopped, dsUSD, dsTopped float64
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
			moonTopped = bal.ToppedUp
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
			dsTopped = bal.ToppedUp
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

	samp := sampleBasis(now)
	if moonOK {
		c.storeSnapshot(config.ProviderMoonshot, samp, now, moonUSD, "USD", moonUSD, moonTopped)
	}
	if dsOK {
		c.storeSnapshot(config.ProviderDeepSeek, samp, now, dsUSD, "USD", dsUSD, dsTopped)
	}
	if zaiOK {
		c.storeSnapshot(config.ProviderZai, samp, now, zaiCredits, "credits", 0, 0)
	}
}

// stalenessWatchdog polls for providers whose newest 'sample' snapshot is older
// than snapshotInterval and triggers an immediate capture when any is stale.
func (c *SnapshotController) stalenessWatchdog(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(stalenessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.staleProvider(ctx, time.Now().UTC()) {
				c.log.Info("snapshot stale, triggering capture")
				c.capture(ctx)
			}
		}
	}
}

// staleProvider reports whether any key-configured provider lacks a 'sample'
// snapshot fresher than snapshotInterval.
func (c *SnapshotController) staleProvider(ctx context.Context, now time.Time) bool {
	providers := []config.Provider{config.ProviderMoonshot, config.ProviderDeepSeek, config.ProviderZai}
	for _, prov := range providers {
		if !c.providerConfigured(prov) {
			continue
		}
		points, err := c.store.SampleSeries(prov, now.Add(-snapshotInterval))
		if err != nil {
			c.log.Warn("snapshot staleness check failed", "provider", prov, "err", err)
			continue
		}
		if len(points) == 0 {
			return true
		}
	}
	return false
}

func (c *SnapshotController) providerConfigured(prov config.Provider) bool {
	var get func() (string, bool)
	switch prov {
	case config.ProviderMoonshot:
		get = c.ks.Moonshot
	case config.ProviderDeepSeek:
		get = c.ks.DeepSeek
	case config.ProviderZai:
		get = c.ks.Zai
	default:
		return false
	}
	if get == nil {
		return false
	}
	_, ok := get()
	return ok
}

func (c *SnapshotController) storeSnapshot(prov config.Provider, b basisEntry, capturedAt time.Time, amount float64, currency string, usd, toppedUp float64) {
	snap := usage.BalanceSnapshot{
		Provider:    prov,
		Basis:       b.basis,
		PeriodStart: b.periodStart,
		CapturedAt:  capturedAt,
		Amount:      amount,
		Currency:    currency,
		USDAmount:   usd,
		ToppedUp:    toppedUp,
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

// sampleBasis returns the single 'sample' basis entry used by capture.
func sampleBasis(t time.Time) basisEntry {
	return basisEntry{basis: "sample", periodStart: t.Truncate(time.Minute)}
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
