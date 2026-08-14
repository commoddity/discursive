package usage

import (
	"fmt"
	"sort"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

// SpendPoint is one balance sample captured at a point in time.
type SpendPoint struct {
	CapturedAt time.Time
	Amount     float64 // native currency (USD, CNY, or credits)
	Currency   string
	USDAmount  float64 // best-effort USD at capture time (fallback math only)
	ToppedUp   float64 // native topped_up component; 0 = unknown
}

// SpendDelta is a single spend delta in USD between two consecutive samples.
type SpendDelta struct {
	At  time.Time
	USD float64
}

// LocalDayStart returns midnight (start of day) of t's day in loc.
// A nil loc means time.Local.
func LocalDayStart(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// SampleSeries loads 'sample' basis balance snapshots for a provider captured
// on or after since. captured_at is stored as an RFC3339Nano string with
// variable-length nanos, so SQL range/order is not relied upon: each timestamp
// is parsed in Go, filtered here, and the result is sorted ascending.
func (s *Store) SampleSeries(provider config.Provider, since time.Time) ([]SpendPoint, error) {
	rows, err := s.db.Query(
		`SELECT captured_at, amount, currency, usd_amount, topped_up_amount
		FROM balance_snapshots
		WHERE provider = ? AND basis = 'sample' AND captured_at >= ?
		ORDER BY captured_at ASC`,
		string(provider), since.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("sample series query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SpendPoint
	for rows.Next() {
		var p SpendPoint
		var tsStr string
		if err := rows.Scan(&tsStr, &p.Amount, &p.Currency, &p.USDAmount, &p.ToppedUp); err != nil {
			return nil, fmt.Errorf("scan spend point: %w", err)
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return nil, fmt.Errorf("parse captured_at in sample series: %w", err)
		}
		if ts.After(since) || ts.Equal(since) {
			p.CapturedAt = ts
			out = append(out, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CapturedAt.Before(out[j].CapturedAt)
	})
	return out, nil
}

// ComputeSpend derives USD spend deltas between consecutive samples. Equal
// CapturedAt timestamps are deduplicated (keeping the last). Spend is the
// decrease in total balance; a recharge (balance increase) is a top-up and is
// clamped to zero, so it is never counted as spend. ToppedUp is informational
// only and is deliberately not part of the delta math.
func ComputeSpend(points []SpendPoint, fxUSDPerCNY float64) []SpendDelta {
	dedup := make(map[time.Time]SpendPoint, len(points))
	order := make([]time.Time, 0, len(points))
	for _, p := range points {
		if _, seen := dedup[p.CapturedAt]; !seen {
			order = append(order, p.CapturedAt)
		}
		dedup[p.CapturedAt] = p
	}
	sort.SliceStable(order, func(i, j int) bool {
		return order[i].Before(order[j])
	})

	var out []SpendDelta
	for i := 0; i+1 < len(order); i++ {
		a := dedup[order[i]]
		b := dedup[order[i+1]]

		decrease := a.Amount - b.Amount
		if decrease < 0 {
			decrease = 0
		}

		usd := decrease
		switch a.Currency {
		case "USD", "":
		case "CNY":
			if fxUSDPerCNY > 0 {
				usd = decrease / fxUSDPerCNY
			} else {
				usd = a.USDAmount - b.USDAmount
				if usd < 0 {
					usd = 0
				}
			}
		default:
			continue
		}
		out = append(out, SpendDelta{At: b.CapturedAt, USD: usd})
	}
	return out
}

// ConfirmedSpendBetween sums USD spend deltas whose newer point falls within
// (after, before]. SampleSeries is loaded from after-24h so that a pair whose
// older point precedes `after` but whose newer point is inside still counts.
func (s *Store) ConfirmedSpendBetween(provider config.Provider, after, before time.Time, fxUSDPerCNY float64) (float64, error) {
	points, err := s.SampleSeries(provider, after.Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	deltas := ComputeSpend(points, fxUSDPerCNY)
	var sum float64
	for _, d := range deltas {
		if d.At.After(after) && !d.At.After(before) {
			sum += d.USD
		}
	}
	return RoundUSD(sum), nil
}
