// Spend endpoint: blends confirmed (balance-delta) spend for Moonshot/DeepSeek
// with estimated (usage-table) spend for Z.AI/Thaura into one per-bucket series.
package usageui

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

// SpendBucketResponse is the /api/spend payload.
type SpendBucketResponse struct {
	Buckets  []SpendBucket `json:"buckets"`
	Covered  bool          `json:"covered"` // true when the window starts at a captured boundary
	Currency string        `json:"currency"`
}

// SpendBucket is one time bucket's spend split by provider. Confirmed =
// moonshot+deepseek (balance-derived); Estimated = thaura (usage-derived).
// Z.AI is deliberately excluded from headline spend totals: it is a flat-fee
// GLM Coding Plan (credit-based, not real per-token money), so its token
// estimate must never inflate the spend number. Z.AI usage is surfaced
// separately in its own balance/credits card.
type SpendBucket struct {
	Bucket    string  `json:"bucket"`
	Confirmed float64 `json:"confirmed"`
	Estimated float64 `json:"estimated"`
	Moonshot  float64 `json:"moonshot"`
	DeepSeek  float64 `json:"deepseek"`
	Zai       float64 `json:"zai"`
	Thaura    float64 `json:"thaura"`
}

// fxUSDPerCNY is the CNY-per-USD rate used to derive confirmed CNY spend.
// Zero defers to each sample's stored usd_amount (captured at the same rate the
// existing fetcher used when persisting), preserving current conversion behavior.
const fxUSDPerCNY = 0.0

const defaultSpendWindowDays = 13

func (s *Server) handleSpend(w http.ResponseWriter, r *http.Request) {
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	if since.IsZero() {
		since = usage.LocalDayStart(time.Now().AddDate(0, 0, -defaultSpendWindowDays), time.Local)
	}

	until, err := parseUntilParam(r)
	if err != nil {
		http.Error(w, "invalid until parameter", http.StatusBadRequest)
		return
	}
	if until.IsZero() {
		until = time.Now()
	}

	bucketMins := parseBucketParam(r)

	// Confirmed spend (balance deltas) for Moonshot + DeepSeek.
	confirmed := make(map[string]SpendBucket)
	for _, prov := range []config.Provider{config.ProviderMoonshot, config.ProviderDeepSeek} {
		points, err := s.store.SampleSeries(prov, since.Add(-24*time.Hour))
		if err != nil {
			slog.Warn("spend sample series failed", "provider", prov, "err", err)
			continue
		}
		for _, d := range usage.ComputeSpend(points, fxUSDPerCNY) {
			if d.At.Before(since) || d.At.After(until) {
				continue
			}
			key := spendBucketKey(d.At, bucketMins)
			b := confirmed[key]
			b.Bucket = key
			if prov == config.ProviderMoonshot {
				b.Moonshot += d.USD
				b.Confirmed += d.USD
			} else {
				b.DeepSeek += d.USD
				b.Confirmed += d.USD
			}
			confirmed[key] = b
		}
	}

	// Estimated spend (usage table) for Thaura only. Z.AI is excluded from the
	// estimated total (flat-fee coding plan); its usage is surfaced separately.
	estRows, err := s.store.QueryProviderEstSince(since, bucketMins)
	if err != nil {
		slog.Warn("spend estimate query failed", "err", err)
		estRows = nil
	}
	est := make(map[string]SpendBucket)
	for _, row := range estRows {
		b := est[row.Bucket]
		b.Bucket = row.Bucket
		switch config.Provider(row.Provider) {
		case config.ProviderZai:
			// Informational only — never counts toward Estimated/headline totals.
			b.Zai += row.EstUSD
		case config.ProviderThaura:
			b.Thaura += row.EstUSD
			b.Estimated += row.EstUSD
		}
		est[row.Bucket] = b
	}

	// Union all bucket keys in chronological order.
	keys := make(map[string]struct{})
	for k := range confirmed {
		keys[k] = struct{}{}
	}
	for k := range est {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	buckets := make([]SpendBucket, 0, len(sorted))
	for _, k := range sorted {
		b := confirmed[k]
		e := est[k]
		b.Bucket = k
		b.Estimated += e.Estimated
		b.Zai += e.Zai
		b.Thaura += e.Thaura
		buckets = append(buckets, b)
	}
	if buckets == nil {
		buckets = []SpendBucket{}
	}

	// Covered: the window starts at a captured balance boundary for every
	// confirmed provider (Moonshot + DeepSeek), i.e. the delta math reflects the
	// full window rather than a partial one. Used to gate "vs last month" style
	// comparisons in the UI.
	covered := true
	for _, prov := range []config.Provider{config.ProviderMoonshot, config.ProviderDeepSeek} {
		ok, err := s.store.PeriodCovered(prov, "sample", since, 2*time.Hour)
		if err != nil {
			slog.Warn("spend coverage check failed", "provider", prov, "err", err)
			ok = false
		}
		if !ok {
			covered = false
			break
		}
	}

	writeJSON(w, SpendBucketResponse{Buckets: buckets, Covered: covered, Currency: "USD"})
}

// spendBucketKey buckets a confirmed-spend delta by local time. bucketMins <= 0
// groups by local calendar day (YYYY-MM-DD); otherwise it floors to the given
// minute interval (YYYY-MM-DDTHH:MM:00).
func spendBucketKey(at time.Time, bucketMins int) string {
	local := at.In(time.Local)
	if bucketMins <= 0 {
		return usage.LocalDayStart(local, time.Local).Format("2006-01-02")
	}
	floor := local.Truncate(time.Duration(bucketMins) * time.Minute)
	return floor.Format("2006-01-02T15:04:00")
}
