package usage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

// BalanceSnapshot records a provider balance at a point in time.
type BalanceSnapshot struct {
	Provider    config.Provider `json:"provider"`
	Basis       string          `json:"basis"`       // "sample", "day", "week", "month"
	PeriodStart time.Time       `json:"periodStart"` // UTC start of this period window
	CapturedAt  time.Time       `json:"capturedAt"`  // UTC when fetched
	Amount      float64         `json:"amount"`      // raw provider value
	Currency    string          `json:"currency"`    // "USD", "CNY", "credits"
	USDAmount   float64         `json:"usdAmount"`   // USD-equivalent (0 for credits)
}

const (
	basisSample = "sample"
	basisDay    = "day"
	basisWeek   = "week"
	basisMonth  = "month"
)

// AllPeriodBases returns every period basis constant (sample excluded from
// confirmed-spend queries — it is the live tick, not a boundary anchor).
func AllPeriodBases() []string {
	return []string{basisDay, basisWeek, basisMonth}
}

// InsertBalanceSnapshot persists a single snapshot row.
func (s *Store) InsertBalanceSnapshot(snap BalanceSnapshot) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO balance_snapshots
		(provider, basis, period_start, captured_at, amount, currency, usd_amount)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(snap.Provider), snap.Basis,
		snap.PeriodStart.UTC().Format(time.RFC3339),
		snap.CapturedAt.UTC().Format(time.RFC3339Nano),
		snap.Amount, snap.Currency, snap.USDAmount,
	)
	if err != nil {
		return fmt.Errorf("insert balance snapshot: %w", err)
	}
	return nil
}

// ConfirmedSpend returns the actual spend for a provider+basis over a given
// time range by comparing the earliest and latest snapshots within that range.
// Returns 0 if fewer than 2 snapshots exist — we need a delta.
func (s *Store) ConfirmedSpend(provider config.Provider, basis string, after, before time.Time) (float64, error) {
	rows, err := s.db.Query(
		`SELECT usd_amount, captured_at
		FROM balance_snapshots
		WHERE provider = ? AND basis = ? AND captured_at >= ? AND captured_at <= ?
		ORDER BY captured_at ASC`,
		string(provider), basis,
		after.UTC().Format(time.RFC3339Nano),
		before.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("confirmed spend query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type row struct {
		usd float64
		ts  time.Time
	}
	var snaps []row
	for rows.Next() {
		var usd float64
		var tsStr string
		if err := rows.Scan(&usd, &tsStr); err != nil {
			return 0, fmt.Errorf("scan confirmed spend: %w", err)
		}
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)
		snaps = append(snaps, row{usd, ts})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows: %w", err)
	}
	if len(snaps) < 2 {
		return 0, nil
	}
	delta := snaps[0].usd - snaps[len(snaps)-1].usd
	if delta < 0 {
		delta = 0
	}
	return delta, nil
}

// DeleteBalanceSnapshotsBefore deletes snapshot rows captured before the cutoff.
func (s *Store) DeleteBalanceSnapshotsBefore(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM balance_snapshots WHERE captured_at < ?`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("delete snapshots before %v: %w", cutoff, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// LatestBalanceSnapshots returns the most recent snapshot per provider+basis.
func (s *Store) LatestBalanceSnapshots() ([]BalanceSnapshot, error) {
	rows, err := s.db.Query(
		`SELECT provider, basis, period_start, captured_at, amount, currency, usd_amount
		FROM balance_snapshots
		WHERE captured_at = (
			SELECT MAX(captured_at)
			FROM balance_snapshots AS inner
			WHERE inner.provider = balance_snapshots.provider
			AND   inner.basis    = balance_snapshots.basis
		)
		ORDER BY provider, basis`)
	if err != nil {
		return nil, fmt.Errorf("latest balance snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanBalanceSnapshots(rows)
}

// BalanceSnapshotsForProvider returns all snapshots for a single provider+basis
// within a time window, ordered ascending by captured_at.
func (s *Store) BalanceSnapshotsForProvider(provider config.Provider, basis string, after time.Time) ([]BalanceSnapshot, error) {
	rows, err := s.db.Query(
		`SELECT provider, basis, period_start, captured_at, amount, currency, usd_amount
		FROM balance_snapshots
		WHERE provider = ? AND basis = ? AND captured_at > ?
		ORDER BY captured_at ASC`,
		string(provider), basis, after.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("snapshots for %q/%s: %w", provider, basis, err)
	}
	defer func() { _ = rows.Close() }()
	return scanBalanceSnapshots(rows)
}

func scanBalanceSnapshots(rows *sql.Rows) ([]BalanceSnapshot, error) {
	var out []BalanceSnapshot
	for rows.Next() {
		var s BalanceSnapshot
		var provStr, periodStr, capStr string
		if err := rows.Scan(&provStr, &s.Basis, &periodStr, &capStr, &s.Amount, &s.Currency, &s.USDAmount); err != nil {
			return nil, fmt.Errorf("scan balance snapshot: %w", err)
		}
		s.Provider = config.Provider(provStr)
		s.PeriodStart, _ = time.Parse(time.RFC3339, periodStr)
		s.CapturedAt, _ = time.Parse(time.RFC3339Nano, capStr)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
