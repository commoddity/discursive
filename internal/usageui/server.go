// Package usageui serves the embedded usage dashboard on a loopback HTTP server.
// It provides /api/* JSON endpoints backed by usage.Store query methods and
// serves static assets (HTML/CSS/JS + Chart.js) from embed/usageui/.
package usageui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"

	"github.com/commoddity/discursive/internal/usage"
)

// HealthInfo holds gateway runtime health data for the dashboard.
type HealthInfo struct {
	Version        string `json:"version"`
	PID            int    `json:"pid"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	HasMoonshotKey bool   `json:"has_moonshot_key"`
	HasDeepSeekKey bool   `json:"has_deepseek_key"`
	TunnelMode     string `json:"tunnel_mode"`
	PublicURL      string `json:"public_url"`
	LocalPort      int    `json:"local_port"`
}

//go:embed static
var staticFS embed.FS

// Server serves the usage dashboard on a loopback listener.
type Server struct {
	addr    string
	store   *usage.Store
	httpSrv *http.Server
	health  HealthInfo
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
	mux.HandleFunc("/api/by-model", s.handleByModel)
	mux.HandleFunc("/api/by-provider", s.handleByProvider)
	mux.HandleFunc("/api/sessions", s.handleSessions)

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
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.health)
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
	days, err := s.store.QueryLastNDays(30)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, days)
}

func (s *Server) handleByModel(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.QueryByModel()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, models)
}

func (s *Server) handleByProvider(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.QueryByProvider()
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
	sessions, err := s.store.QuerySessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
