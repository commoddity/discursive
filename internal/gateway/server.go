package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway/verbosity"
	"github.com/commoddity/discursive/internal/gateway/vision"
	"github.com/commoddity/discursive/internal/usage"
)

// Auxiliary worker usage events get their own session/request ids so they are
// distinguishable from (and don't pollute) the active chat session grouping.
const (
	auxVisionSession     = "vision-worker"
	auxCompressSession   = "compressor-worker"
	auxVisionRequestID   = "vision"
	auxCompressRequestID = "compressor"
)

// ServerConfig configures the local OpenAI-compatible gateway.
type ServerConfig struct {
	ListenAddr            string // e.g. "127.0.0.1:4001"
	GatewayKey            string
	DataRoot              string
	Settings              *config.AppSettings
	Live                  *config.LiveSettings // optional; when set, drives per-model reasoning effort
	HTTPClient            *http.Client
	ChatURLOverride       map[config.Provider]string // tests only
	VisionChatURLOverride string                     // tests only
	CompressEnabled       bool                       // default false (usage dashboard toggle)
	SubAgentRouterEnabled bool                       // default true in production (CLI --subagent-router)
}

// Server is the loopback gateway HTTP server.
type Server struct {
	cfg         ServerConfig
	mux         *http.ServeMux
	httpSrv     *http.Server
	client      *http.Client
	store       *usage.Store
	agg         *usage.Aggregator
	sessionID   string
	settings    *config.AppSettings
	live        *config.LiveSettings
	vision      *vision.Describer
	visionCache interface{ Close() error } // durable image-description cache; nil in tests
	router      *SubAgentRouter
	compressor  *Compressor
	verbosity   *verbosity.Controller

	mu       sync.Mutex
	listener net.Listener
}

// NewServer builds a gateway server (does not listen yet).
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Settings == nil && cfg.Live == nil {
		return nil, fmt.Errorf("gateway: Settings or Live required")
	}
	if cfg.Settings == nil {
		snap := cfg.Live.Snapshot()
		cfg.Settings = &snap
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
		live:      cfg.Live,
	}
	s.vision = vision.NewDescriber(s.client)
	visionCache, err := vision.NewPersistentCache(filepath.Join(cfg.DataRoot, "usage"))
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("gateway: open vision cache: %w", err)
	}
	s.vision.SetPersistentCache(visionCache)
	s.visionCache = visionCache
	s.vision.SetUsageRecorder(func(provider config.Provider, model string, promptTokens, completionTokens uint64, latency time.Duration) {
		s.recordAuxUsage(auxVisionSession, provider, model, auxVisionRequestID, latency, tokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
		})
	})
	s.router = NewSubAgentRouter(SubAgentRouterConfig{
		Enabled: cfg.SubAgentRouterEnabled,
	})

	s.compressor = NewCompressor(CompressorConfig{
		Enabled: true,
		RecordUsage: func(provider config.Provider, model string, promptTokens, completionTokens uint64, latency time.Duration) {
			s.recordAuxUsage(auxCompressSession, provider, model, auxCompressRequestID, latency, tokenUsage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
			})
		},
	}, s.client)

	s.verbosity = verbosity.NewController(verbosity.VerbosityConfig{
		Models: map[string]verbosity.ModelConfig{
			"kimi-k3": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              proVerbosityMaxTokens,
			},
			"kimi-k2.7-code": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              flashVerbosityMaxTokens,
			},
			"deepseek-v4-flash": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              flashVerbosityMaxTokens,
			},
			"deepseek-v4-pro": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              proVerbosityMaxTokens,
			},
			config.ModelOpenRouterDeepSeekV4Flash: {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              flashVerbosityMaxTokens,
			},
			config.ModelOpenRouterDeepSeekV4Pro: {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              proVerbosityMaxTokens,
			},
			"glm-4.7": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmVerbosityMaxTokens,
			},
			"glm-5.3": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmMaxVerbosityMaxTokens,
			},
			"glm-5.3[1m]": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmMaxVerbosityMaxTokens,
			},
			config.ModelOpenRouterZaiGLM53: {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmMaxVerbosityMaxTokens,
			},
			config.ModelOpenRouterZaiGLM47: {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmVerbosityMaxTokens,
			},
			"thaura": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              proVerbosityMaxTokens,
			},
		},
	})
	s.routes()
	return s, nil
}

// toolCompressionEnabled reports whether tool-result compression is currently
// on. It reads the live runtime setting when present, falling back to the
// static ServerConfig flag or persisted settings for test/no-live setups.
func (s *Server) toolCompressionEnabled() bool {
	if s.live != nil {
		return s.live.ToolCompressionEnabled()
	}
	if s.cfg.CompressEnabled {
		return true
	}
	return s.settings != nil && s.settings.ToolCompressionEnabled
}

func (s *Server) routes() {
	auth := func(h http.HandlerFunc) http.Handler {
		return AuthMiddleware(h, s.cfg.GatewayKey)
	}

	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.Handle("GET /v1/models", auth(s.handleModels))
	s.mux.Handle("POST /v1/models", auth(s.handleModels))
	s.mux.Handle("POST /v1/chat/completions", auth(s.handleChatCompletions))
	s.mux.Handle("POST /v1/responses", auth(s.handleChatCompletions))
}

// Handler returns the HTTP handler for httptest tests.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// SessionID returns the process session id used for usage events.
func (s *Server) SessionID() string {
	return s.sessionID
}

// Store returns the usage store for external consumers (usage UI).
func (s *Server) Store() *usage.Store {
	return s.store
}

// ListenAndServe binds 127.0.0.1 and serves until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.ListenAddr, err)
	}
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
	if s.visionCache != nil {
		_ = s.visionCache.Close()
	}
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) chatURL(provider config.Provider, model string) (string, error) {
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
	case config.ProviderThaura:
		k, err := s.settings.GetThauraKey(s.cfg.DataRoot)
		if err != nil {
			return "", err
		}
		if k == nil || *k == "" {
			return "", fmt.Errorf("API key not configured for this model")
		}
		return *k, nil
	case config.ProviderZai:
		k, err := s.settings.GetZaiKey(s.cfg.DataRoot)
		if err != nil {
			return "", err
		}
		if k == nil || *k == "" {
			return "", fmt.Errorf("API key not configured for this model")
		}
		return *k, nil
	case config.ProviderOpenRouter:
		k, err := s.settings.GetOpenRouterKey(s.cfg.DataRoot)
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

func (s *Server) compressContext(provider config.Provider, at time.Time) (CompressContext, error) {
	spec, ok := config.ProviderSpecFor(provider)
	if !ok {
		return CompressContext{}, fmt.Errorf("unknown provider %q", provider)
	}
	model := spec.SmallModel
	routeProvider := provider
	if spec.HasPeak && peakNow(model, at) && s.settings != nil && s.settings.HasOpenRouterKey() {
		if twin, ok := config.OpenRouterTwinFor(model); ok {
			model = twin
			routeProvider = config.ProviderOpenRouter
		}
	}
	chatURL, err := s.chatURL(routeProvider, model)
	if err != nil {
		return CompressContext{}, err
	}
	key, err := s.upstreamKey(routeProvider)
	if err != nil {
		return CompressContext{}, err
	}
	return CompressContext{
		Provider: routeProvider,
		Model:    model,
		ChatURL:  chatURL,
		APIKey:   key,
	}, nil
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

func (s *Server) sanitizeConfig() SanitizeConfig {
	scfg := DefaultSanitizeConfig()
	if s.live != nil {
		scfg.EffortByModel = s.live.EffortMap()
		scfg.ThinkingEnabledByModel = s.live.ThinkingEnabledMap()
	} else if s.settings != nil {
		scfg.EffortByModel = config.NormalizeReasoningEffortMap(s.settings.ReasoningEffort)
		scfg.ThinkingEnabledByModel = config.NormalizeThinkingEnabledMap(s.settings.ThinkingEnabled)
	}
	return scfg
}

func logRequest(requestID string, attrs ...any) {
	args := append([]any{"request_id", requestID}, attrs...)
	slog.Info("gateway_request", args...)
}
