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
	"strings"
	"sync"
	"sync/atomic"
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
	cfg             ServerConfig
	mux             *http.ServeMux
	httpSrv         *http.Server
	client          *http.Client
	store           *usage.Store
	agg             *usage.Aggregator
	sessionID       string
	settings        *config.AppSettings
	live            *config.LiveSettings
	vision          *vision.Describer
	visionCache     interface{ Close() error } // durable image-description cache; nil in tests
	router          *SubAgentRouter
	compressor      *Compressor
	verbosity       *verbosity.Controller
	zaiSem          chan struct{}    // limits concurrent Z.AI requests (plan concurrency)
	glm47Lane       chan struct{}    // glm-4.7 downgrade lane (cap = plan concurrency limit)
	glm47LaneInUse  atomic.Int64     // concurrent in-flight requests using the glm-4.7 lane
	stickyFallbacks *stickyFallbacks // per-model sticky fallback after lane overflow

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
	// Wire vision describer when Z.AI settings are available (graceful nil
	// when unset — images just fall through to existing strip behavior).
	// Use the coding-plan endpoint (not on-demand) since the user's Z.AI key
	// is a GLM Coding Plan key and glm-4.6v is available on that plan.
	visionURL, _ := config.ChatCompletionsURL(config.ProviderZai)
	if cfg.VisionChatURLOverride != "" {
		visionURL = cfg.VisionChatURLOverride
	}
	zaiKeyFn := func() (string, bool) {
		k, err := s.settings.GetZaiKey(s.cfg.DataRoot)
		if err != nil || k == nil || *k == "" {
			return "", false
		}
		return *k, true
	}
	s.vision = vision.NewDescriber(s.client, visionURL, zaiKeyFn)
	// Vision calls hit the same Z.AI coding-plan endpoint, so they must share
	// the plan-concurrency budget with chat: block on a zaiSem slot before each
	// describe (with a correct no-op release when the ctx expired unacquired).
	s.vision.SetPlanSlotter(func(ctx context.Context) func() {
		if s.zaiSem == nil {
			return func() {}
		}
		select {
		case s.zaiSem <- struct{}{}:
			return func() { <-s.zaiSem }
		case <-ctx.Done():
			return func() {}
		}
	})
	// Install a durable description cache so historical images (resent by
	// Cursor on every turn) are resolved without re-invoking the vision model.
	// This keeps a rate-limited vision provider from breaking later turns that
	// only carry already-described images.
	visionCache, err := vision.NewPersistentCache(filepath.Join(cfg.DataRoot, "usage"))
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("gateway: open vision cache: %w", err)
	}
	s.vision.SetPersistentCache(visionCache)
	s.visionCache = visionCache
	// Best-effort usage metering for vision calls: they use real LLM tokens but
	// were previously unrecorded. Meter into a dedicated sentinel session so the
	// cost accrues without polluting the active chat session grouping.
	s.vision.SetUsageRecorder(func(model string, promptTokens, completionTokens uint64, latency time.Duration) {
		s.recordAuxUsage(auxVisionSession, config.ProviderZai, model, auxVisionRequestID, latency, tokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
		})
	})
	s.router = NewSubAgentRouter(SubAgentRouterConfig{
		Enabled: cfg.SubAgentRouterEnabled,
		// Lane selection for glm-4.7 downgrades: first N concurrent requests
		// stay on glm-4.7 (plan concurrency limit), overflow goes elsewhere.
		PickDowngradeModel: func(candidate, requestID string) (string, func()) {
			// Router downgrades now always target OpenRouter DeepSeek flash,
			// never glm-4.7. The glm-4.7 lane is only for direct glm-4.7
			// main-chat concurrency.
			if candidate != "glm-4.7" {
				return candidate, nil
			}
			return s.selectDowngradeLane(requestID)
		},
	})

	// Z.AI concurrency limiter: glm-4.7 plan concurrency is 2; the semaphore
	// blocks excess Z.AI traffic until a slot frees (instead of letting it
	// hit upstream and eat a guaranteed 429 + retry cycle).
	s.zaiSem = make(chan struct{}, glm47LaneCap)
	// glm-4.7 downgrade lane: downgrade selection is non-blocking — overflow
	// is delegated to another lane (see selectDowngradeLane) instead of queueing.
	s.glm47Lane = make(chan struct{}, glm47LaneCap)
	s.stickyFallbacks = newStickyFallbacks()

	// Always allocate the compressor and verbosity controller so they can be
	// enabled/disabled at runtime from the usage dashboard without a restart.
	// Per-request gates (compressionEnabled / verbosityEnabled) read the live
	// setting; when disabled they short-circuit before any work is done, so the
	// always-allocated structs add zero hot-path cost.
	// Compression worker targets OpenRouter DeepSeek flash when an OpenRouter
	// key is configured, otherwise falls back to direct DeepSeek flash.
	compressURL, _ := config.ChatCompletionsURL(config.ProviderOpenRouter)
	// Strip /chat/completions suffix — Compressor.ChatURL expects the base URL.
	flashBase := strings.TrimSuffix(compressURL, "/chat/completions")
	flashKeyFn := func() (string, bool) {
		if k, err := s.settings.GetOpenRouterKey(s.cfg.DataRoot); err == nil && k != nil && *k != "" {
			return *k, true
		}
		k, err := s.settings.GetDeepSeekKey(s.cfg.DataRoot)
		if err != nil || k == nil || *k == "" {
			return "", false
		}
		return *k, true
	}
	s.compressor = NewCompressor(CompressorConfig{
		Enabled:   true,
		ChatURL:   flashBase,
		GetAPIKey: flashKeyFn,
		// Best-effort usage metering for summarizer calls: they use real LLM
		// tokens but were previously unrecorded. Meter into a dedicated
		// sentinel session so the cost accrues without polluting the active
		// chat session grouping.
		RecordUsage: func(model string, promptTokens, completionTokens uint64, latency time.Duration) {
			provider := config.ProviderOpenRouter
			if model == "deepseek-v4-flash" {
				provider = config.ProviderDeepSeek
			}
			s.recordAuxUsage(auxCompressSession, provider, model, auxCompressRequestID, latency, tokenUsage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
			})
		},
	}, s.client)

	// Verbosity controller holds the per-model verbosity config (terseness
	// directive, token cap) for every model that supports it. Whether a model's
	// controls are actually applied is decided per-request by
	// verbosityEnabledFor, which reads the live per-model toggles. Verbosity
	// only coerces the model via the request — response content is never edited.
	s.verbosity = verbosity.NewController(verbosity.VerbosityConfig{
		Models: map[string]verbosity.ModelConfig{
			"deepseek-v4-flash": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              flashVerbosityMaxTokens,
			},
			"deepseek-v4-pro": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              proVerbosityMaxTokens,
			},
			"deepseek/deepseek-v4-flash-0731": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              flashVerbosityMaxTokens,
			},
			"deepseek/deepseek-v4-pro-0813": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              proVerbosityMaxTokens,
			},
			"glm-4.7": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmVerbosityMaxTokens,
			},
			// glm-5.3[1m] normalizes to glm-5.3 — registered under both ids so
			// the controller lookup hits regardless of suffix presence.
			"glm-5.3": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmMaxVerbosityMaxTokens,
			},
			"glm-5.3[1m]": {
				SystemMessageDirective: flashTersenessDirective,
				MaxTokens:              glmMaxVerbosityMaxTokens,
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

// verbosityEnabledFor reports whether output-verbosity controls are on for a
// real model id. It reads the live per-model map when present, falling back to
// the persisted settings map. Flash defaults to on; other models default per
// the catalog.
func (s *Server) verbosityEnabledFor(model string) bool {
	if s.live != nil {
		return s.live.VerbosityFor(model)
	}
	if s.settings != nil {
		return config.VerbosityFor(s.settings.Verbosity, model)
	}
	return false
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
