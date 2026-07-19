package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"discursive/internal/config"
	"discursive/internal/usage"
)

// ServerConfig configures the local OpenAI-compatible gateway.
type ServerConfig struct {
	ListenAddr      string // e.g. "127.0.0.1:4001"
	GatewayKey      string
	DataRoot        string
	Settings        *config.AppSettings
	HTTPClient      *http.Client
	ChatURLOverride map[config.Provider]string // tests only
}

// Server is the loopback gateway HTTP server.
type Server struct {
	cfg       ServerConfig
	mux       *http.ServeMux
	httpSrv   *http.Server
	client    *http.Client
	store     *usage.Store
	agg       *usage.Aggregator
	sessionID string
	settings  *config.AppSettings

	mu       sync.Mutex
	listener net.Listener
}

// NewServer builds a gateway server (does not listen yet).
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Settings == nil {
		return nil, fmt.Errorf("gateway: Settings required")
	}
	if cfg.GatewayKey == "" {
		cfg.GatewayKey = cfg.Settings.GatewayKey
	}
	if cfg.ListenAddr == "" {
		port := cfg.Settings.LocalPort
		if port == 0 {
			port = config.DefaultPort
		}
		cfg.ListenAddr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	store, err := usage.NewStore(cfg.DataRoot)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:       cfg,
		mux:       http.NewServeMux(),
		client:    cfg.HTTPClient,
		store:     store,
		agg:       usage.NewAggregator(0),
		sessionID: newSessionID(),
		settings:  cfg.Settings,
	}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("POST /v1/responses", s.handleChatCompletions)
}

// Handler returns the HTTP handler for httptest tests.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// SessionID returns the process session id used for usage events.
func (s *Server) SessionID() string {
	return s.sessionID
}

// ListenAndServe binds 127.0.0.1 and serves until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.ListenAddr, err)
	}
	// Refuse non-loopback binds if somehow misconfigured.
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok && tcp.IP != nil && !tcp.IP.IsLoopback() {
		_ = ln.Close()
		return fmt.Errorf("refusing non-loopback listen addr %s", s.cfg.ListenAddr)
	}
	s.mu.Lock()
	s.listener = ln
	s.httpSrv = &http.Server{Handler: s.mux}
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
		err := <-errCh
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// Shutdown stops the HTTP server and flushes the usage aggregator.
func (s *Server) Shutdown(ctx context.Context) error {
	s.agg.Stop()
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) chatURL(provider config.Provider) (string, error) {
	if s.cfg.ChatURLOverride != nil {
		if u, ok := s.cfg.ChatURLOverride[provider]; ok && u != "" {
			return u, nil
		}
	}
	return config.ChatCompletionsURL(provider)
}

func (s *Server) upstreamKey(provider config.Provider) (string, error) {
	switch provider {
	case config.ProviderMoonshot:
		k, err := s.settings.GetMoonshotKey(s.cfg.DataRoot)
		if err != nil {
			return "", err
		}
		if k == nil || *k == "" {
			return "", fmt.Errorf("API key not configured for this model")
		}
		return *k, nil
	case config.ProviderDeepSeek:
		k, err := s.settings.GetDeepSeekKey(s.cfg.DataRoot)
		if err != nil {
			return "", err
		}
		if k == nil || *k == "" {
			return "", fmt.Errorf("API key not configured for this model")
		}
		return *k, nil
	default:
		return "", fmt.Errorf("unsupported model")
	}
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	return "sess_" + hex.EncodeToString(b[:])
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return "req_" + hex.EncodeToString(b[:])
}

func logRequest(requestID string, attrs ...any) {
	args := append([]any{"request_id", requestID}, attrs...)
	slog.Info("gateway_request", args...)
}
