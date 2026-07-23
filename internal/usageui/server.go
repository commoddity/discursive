// Package usageui serves the embedded usage dashboard on a loopback HTTP server.
// It provides /api/* JSON endpoints backed by usage.Store query methods and
// serves static assets (HTML/CSS/JS + Chart.js) from embed/usageui/.
package usageui

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

// HealthInfo holds gateway runtime health data for the dashboard.
type HealthInfo struct {
	Version        string `json:"version"`
	PID            int    `json:"pid"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	StartedAt      string `json:"started_at"`
	HasMoonshotKey bool   `json:"has_moonshot_key"`
	HasDeepSeekKey bool   `json:"has_deepseek_key"`
	HasThauraKey   bool   `json:"has_thaura_key"`
	TunnelMode     string `json:"tunnel_mode"`
	PublicURL      string `json:"public_url"`
	LocalPort      int    `json:"local_port"`
	GatewayKey     string `json:"gateway_key"`
}

//go:embed static
var staticFS embed.FS

// Server serves the usage dashboard on a loopback listener.
type Server struct {
	addr       string
	store      *usage.Store
	httpSrv    *http.Server
	health     HealthInfo
	startTime  time.Time
	keySource  KeySource
	httpClient *http.Client // optional; tests inject a mock transport
	live       *config.LiveSettings
}

// NewServer creates a usage UI server backed by the given store.
func NewServer(addr string, store *usage.Store) *Server {
	if addr == "" {
		addr = "127.0.0.1:4002"
	}
	return &Server{addr: addr, store: store}
}

// Start begins serving on the configured loopback address.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Static files served from embed under /static/.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Index page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})

	// API endpoints.
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/by-day", s.handleByDay)
	mux.HandleFunc("/api/by-day-model", s.handleByDayModel)
	mux.HandleFunc("/api/by-model", s.handleByModel)
	mux.HandleFunc("/api/by-provider", s.handleByProvider)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/exchange-rate", s.handleExchangeRate)
	mux.HandleFunc("/api/balances", s.handleBalances)
	mux.HandleFunc("/api/reasoning-effort", s.handleReasoningEffort)

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.httpSrv = &http.Server{Handler: mux}
	slog.Info("usage_ui_started", "addr", s.addr)
	go func() { _ = s.httpSrv.Serve(ln) }()
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	if s.httpSrv != nil {
		return s.httpSrv.Close()
	}
	return nil
}

// SetHealth sets the runtime health info displayed on the dashboard.
func (s *Server) SetHealth(h HealthInfo) {
	s.health = h
	s.startTime = time.Now()
}

// SetLive wires app settings for reasoning-effort load/save.
func (s *Server) SetLive(live *config.LiveSettings) {
	s.live = live
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	h := s.health
	if !s.startTime.IsZero() {
		h.UptimeSeconds = int64(time.Since(s.startTime).Seconds())
		h.StartedAt = s.startTime.UTC().Format(time.RFC3339)
	}
	writeJSON(w, h)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ds, err := s.store.QueryMonthToDate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, ds)
}

func (s *Server) handleByDay(w http.ResponseWriter, r *http.Request) {
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	if since.IsZero() {
		days, err := s.store.QueryLastNDays(30)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, days)
		return
	}
	bucketMins := parseBucketParam(r)
	days, err := s.store.QueryByDaySince(since, bucketMins)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, padBuckets(since, time.Now().UTC(), bucketMins, days))
}

func (s *Server) handleByDayModel(w http.ResponseWriter, r *http.Request) {
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	if since.IsZero() {
		http.Error(w, "since parameter required", http.StatusBadRequest)
		return
	}
	until, err := parseUntilParam(r)
	if err != nil {
		http.Error(w, "invalid until parameter", http.StatusBadRequest)
		return
	}
	if until.IsZero() {
		until = time.Now().UTC()
	}
	bucketMins := parseBucketParam(r)
	rows, err := s.store.QueryByDayModelSince(since, bucketMins)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Pad with empty slots so the frontend always gets contiguous bucket slots.
	// We produce an index of existing (bucket, model) pairs and then fill gaps.
	type modelRow struct {
		Provider string
		Model    string
	}
	bucketDur := time.Duration(bucketMins) * time.Minute
	if bucketMins <= 0 {
		bucketDur = 24 * time.Hour
	}
	floorSince := since.Truncate(bucketDur)
	floorUntil := until.Truncate(bucketDur)

	// Collect distinct models and index existing data.
	modelSet := make(map[string]modelRow) // "provider::model" -> row
	type bucketEntry struct {
		Cost      float64
		ReqCount  uint64
		TokensIn  uint64
		TokensOut uint64
		CacheHit  uint64
		CacheMiss uint64
	}
	existing := make(map[string]map[string]bucketEntry) // bucket -> "provider::model" -> entry
	for _, r := range rows {
		key := r.Provider + "::" + r.Model
		modelSet[key] = modelRow{Provider: r.Provider, Model: r.Model}
		if existing[r.Bucket] == nil {
			existing[r.Bucket] = make(map[string]bucketEntry)
		}
		existing[r.Bucket][key] = bucketEntry{
			Cost:      r.EstUSD,
			ReqCount:  r.RequestCount,
			TokensIn:  r.TokensIn,
			TokensOut: r.TokensOut,
			CacheHit:  r.CacheHitTokens,
			CacheMiss: r.CacheMissTokens,
		}
	}

	// Build sorted model keys for consistent ordering.
	var modelKeys []string
	for k := range modelSet {
		modelKeys = append(modelKeys, k)
	}
	sort.Strings(modelKeys)

	// Produce the flattened output: for each bucket slot, emit zero values for every model.
	type FlatRow struct {
		Bucket          string  `json:"bucket"`
		Provider        string  `json:"provider"`
		Model           string  `json:"model"`
		EstUSD          float64 `json:"est_usd"`
		RequestCount    uint64  `json:"request_count"`
		TokensIn        uint64  `json:"tokens_in"`
		TokensOut       uint64  `json:"tokens_out"`
		CacheHitTokens  uint64  `json:"cache_hit_tokens"`
		CacheMissTokens uint64  `json:"cache_miss_tokens"`
	}
	var flat []FlatRow
	for t := floorSince; !t.After(floorUntil); t = t.Add(bucketDur) {
		bucketKey := ""
		if bucketMins > 0 {
			bucketKey = t.Format("2006-01-02T15:04:00")
		} else {
			bucketKey = t.Format("2006-01-02")
		}
		if len(modelKeys) == 0 {
			// No models in range — still emit one zero row per bucket so the
			// client can render an empty axis grid (e.g. Today with no usage yet).
			flat = append(flat, FlatRow{Bucket: bucketKey})
			continue
		}
		for _, mk := range modelKeys {
			mr := modelSet[mk]
			be := bucketEntry{}
			if m, ok := existing[bucketKey]; ok {
				be = m[mk]
			}
			flat = append(flat, FlatRow{
				Bucket:          bucketKey,
				Provider:        mr.Provider,
				Model:           mr.Model,
				EstUSD:          be.Cost,
				RequestCount:    be.ReqCount,
				TokensIn:        be.TokensIn,
				TokensOut:       be.TokensOut,
				CacheHitTokens:  be.CacheHit,
				CacheMissTokens: be.CacheMiss,
			})
		}
	}
	writeJSON(w, flat)
}

func (s *Server) handleByModel(w http.ResponseWriter, r *http.Request) {
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	var models []usage.ModelBreakdown
	if since.IsZero() {
		models, err = s.store.QueryByModel()
	} else {
		models, err = s.store.QueryByModelSince(since)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, models)
}

func (s *Server) handleByProvider(w http.ResponseWriter, r *http.Request) {
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	var providers []usage.ProviderBreakdown
	if since.IsZero() {
		providers, err = s.store.QueryByProvider()
	} else {
		providers, err = s.store.QueryByProviderSince(since)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, providers)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	if sid != "" {
		ds, err := s.store.QuerySessionDetail(sid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, ds)
		return
	}
	since, err := parseSinceParam(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	sessions, err := s.store.QuerySessionsSince(since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.QueryStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Attach DB file size from the filesystem.
	if fi, err := os.Stat(s.store.DBPath()); err == nil {
		stats.DBFileSize = fi.Size()
	}
	writeJSON(w, stats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// parseSinceParam extracts an optional ISO-8601 since timestamp from query params.
func parseSinceParam(r *http.Request) (time.Time, error) {
	s := r.URL.Query().Get("since")
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func parseUntilParam(r *http.Request) (time.Time, error) {
	s := r.URL.Query().Get("until")
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// parseBucketParam extracts an optional bucket duration from the query string.
// Accepted values: "10m", "20m", "1h", "2h", "24h", "1d". Returns minutes.
func parseBucketParam(r *http.Request) int {
	switch r.URL.Query().Get("bucket") {
	case "10m":
		return 10
	case "20m":
		return 20
	case "1h":
		return 60
	case "2h":
		return 120
	case "24h":
		return 1440
	case "1d":
		return 0 // group by date(), not minute-bucket
	default:
		return 0
	}
}

// padBuckets fills in missing bucket slots with zero-value DailySummary entries
// so the client always sees a full grid of N empty bars for the selected timescale.
func padBuckets(since, until time.Time, bucketMins int, actual []usage.DailySummary) []usage.DailySummary {
	if bucketMins <= 0 {
		// Daily bucketing.
		start := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)
		end := time.Date(until.Year(), until.Month(), until.Day(), 0, 0, 0, 0, time.UTC)
		existing := make(map[string]usage.DailySummary, len(actual))
		for _, d := range actual {
			existing[d.Date] = d
		}
		var out []usage.DailySummary
		for t := start; !t.After(end); t = t.AddDate(0, 0, 1) {
			key := t.Format("2006-01-02")
			if ds, ok := existing[key]; ok {
				out = append(out, ds)
			} else {
				out = append(out, usage.DailySummary{Date: key})
			}
		}
		return out
	}
	// Sub-day bucketing (N-minute slots).
	bucketDur := time.Duration(bucketMins) * time.Minute
	// floor since and until to the bucket boundary.
	floorSince := since.Truncate(bucketDur)
	floorUntil := until.Truncate(bucketDur)
	existing := make(map[string]usage.DailySummary, len(actual))
	for _, d := range actual {
		existing[d.Date] = d
	}
	var out []usage.DailySummary
	for t := floorSince; !t.After(floorUntil); t = t.Add(bucketDur) {
		key := t.Format("2006-01-02T15:04:05")[:16] + ":00" // "2006-01-02T15:04:00"
		if ds, ok := existing[key]; ok {
			out = append(out, ds)
		} else {
			out = append(out, usage.DailySummary{Date: key})
		}
	}
	return out
}

// handleExchangeRate proxies the frankfurter.app EUR/USD + CNY/USD rates to avoid CORS issues.
func (s *Server) handleExchangeRate(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get("https://api.frankfurter.app/latest?from=USD&to=EUR,CNY")
	if err != nil {
		http.Error(w, "exchange rate unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
