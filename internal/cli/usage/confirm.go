package usage

import (
	"fmt"
	"io"
	"time"

	"github.com/commoddity/discursive/internal/config"
	usagepkg "github.com/commoddity/discursive/internal/usage"
)

// fxUSDPerCNY is 0 to preserve each balance snapshot's stored usd_amount (the
// same convention the dashboard /api/spend endpoint uses).
const fxUSDPerCNY = 0.0

// providerSpendRow is one line of the confirmed+estimated spend report.
type providerSpendRow struct {
	Provider string
	MTD      float64
	Today    float64
	Basis    string // confirmed | estimated
}

// confirmedSpendReport computes confirmed (Moonshot/DeepSeek balance deltas)
// and estimated (Z.AI/Thaura usage-table) spend for month-to-date and today,
// in the given local time zone. now is provided for deterministic tests.
func confirmedSpendReport(store *usagepkg.Store, now time.Time, loc *time.Location) ([]providerSpendRow, float64, float64, float64, error) {
	monthStart := usagepkg.LocalDayStart(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc), loc)
	dayStart := usagepkg.LocalDayStart(now, loc)

	rows := []providerSpendRow{
		{Provider: "Moonshot", Basis: "confirmed"},
		{Provider: "DeepSeek", Basis: "confirmed"},
		{Provider: "Z.AI", Basis: "estimated"},
		{Provider: "Thaura", Basis: "estimated"},
	}

	confirmed := map[config.Provider]*providerSpendRow{
		config.ProviderMoonshot: &rows[0],
		config.ProviderDeepSeek: &rows[1],
	}
	for prov, row := range confirmed {
		mtd, err := store.ConfirmedSpendBetween(prov, monthStart, now, fxUSDPerCNY)
		if err != nil {
			return nil, 0, 0, 0, fmt.Errorf("confirmed %s MTD: %w", prov, err)
		}
		today, err := store.ConfirmedSpendBetween(prov, dayStart, now, fxUSDPerCNY)
		if err != nil {
			return nil, 0, 0, 0, fmt.Errorf("confirmed %s today: %w", prov, err)
		}
		row.MTD = mtd
		row.Today = today
	}

	estRows, err := store.QueryProviderEstSince(monthStart, 0)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("estimated spend: %w", err)
	}
	estimated := map[config.Provider]*providerSpendRow{
		config.ProviderZai:    &rows[2],
		config.ProviderThaura: &rows[3],
	}
	for prov, row := range estimated {
		mtd, today := sumEstWindow(estRows, prov, monthStart, dayStart, loc)
		row.MTD = mtd
		row.Today = today
	}

	var totalMTD, estMTD, totalToday float64
	for i := range rows {
		if rows[i].Basis == "estimated" && rows[i].Provider == "Z.AI" {
			// Z.AI is a flat-fee coding plan: shown as its own row but excluded
			// from headline totals like the dashboard /api/spend endpoint.
			continue
		}
		totalMTD += rows[i].MTD
		totalToday += rows[i].Today
		if rows[i].Basis == "estimated" {
			estMTD += rows[i].MTD
		}
	}
	return rows, totalMTD, estMTD, totalToday, nil
}

// sumEstWindow sums estimated USD for prov from estRows whose local bucket date
// falls in [after, before]. mtds include everything on or after the month start;
// today is limited to the current local day. Days beyond today (future) are
// excluded.
func sumEstWindow(rows []usagepkg.ProviderEstBucket, prov config.Provider, monthStart, dayStart time.Time, loc *time.Location) (mtd, today float64) {
	monthStr := monthStart.Format("2006-01-02")
	dayStr := dayStart.Format("2006-01-02")
	for _, r := range rows {
		if config.Provider(r.Provider) != prov {
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", r.Bucket, loc)
		if err != nil {
			continue
		}
		ds := d.Format("2006-01-02")
		if ds < monthStr || ds > dayStr {
			continue
		}
		mtd += r.EstUSD
		if ds == dayStr {
			today += r.EstUSD
		}
	}
	return mtd, today
}

// printConfirmedReport writes the confirmed+estimated spend table to w.
func printConfirmedReport(w io.Writer, store *usagepkg.Store, now time.Time, loc *time.Location) error {
	rows, totalMTD, estMTD, totalToday, err := confirmedSpendReport(store, now, loc)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "\n💰 Confirmed spend (balance snapshots) + estimated (usage tokens):\n\n")
	_, _ = fmt.Fprintf(w, "%-12s %12s %12s   %-10s\n", "PROVIDER", "MTD", "TODAY", "BASIS")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%-12s %11.2f %11.2f   %-10s\n", r.Provider, r.MTD, r.Today, r.Basis)
	}
	_, _ = fmt.Fprintf(w, "%-12s %11.2f %11.2f\n", "Total", totalMTD, totalToday)
	_, _ = fmt.Fprintf(w, "\nTotal MTD $%.2f includes $%.2f est. (Thaura tokens)\n", totalMTD, estMTD)
	_, _ = fmt.Fprintf(w, "Z.AI usage is shown separately above and excluded from totals (flat-fee coding plan).\n")
	return nil
}
