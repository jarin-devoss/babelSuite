package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/babelsuite/babelsuite/internal/agent"
	"github.com/babelsuite/babelsuite/internal/auth"
	"github.com/babelsuite/babelsuite/internal/cachehub"
	"github.com/babelsuite/babelsuite/internal/catalog"
	"github.com/babelsuite/babelsuite/internal/cronjobs"
	"github.com/babelsuite/babelsuite/internal/engine"
	enginewatchers "github.com/babelsuite/babelsuite/internal/engine/watchers"
	"github.com/babelsuite/babelsuite/internal/environments"
	"github.com/babelsuite/babelsuite/internal/execution"
	"github.com/babelsuite/babelsuite/internal/httpserver"
	"github.com/babelsuite/babelsuite/internal/mocking"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/profiles"
	"github.com/babelsuite/babelsuite/internal/store"
	mongostore "github.com/babelsuite/babelsuite/internal/store/mongo"
	"github.com/babelsuite/babelsuite/internal/suites"
	"github.com/babelsuite/babelsuite/internal/telemetry"
)

// App is a fully wired, ready-to-serve babelSuite server.
// Use New to create one, ServeHTTP to handle requests, and Close to release resources.
type App struct {
	handler            http.Handler
	telemetryPipeline  *telemetry.Pipeline
	cacheLayer         *cachehub.Hub
	primaryStore       store.Store
	executionService   *execution.Service
	environmentService *environments.Service
	cronService        *cronjobs.Service
	stopBackground     context.CancelFunc
}

// New wires all services and returns a ready App. Health endpoints (/healthz, /readyz) are always included.
func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWTSecret is required")
	}

	primaryStore, err := newMongoStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	telemetryPipeline, err := telemetry.Start(ctx)
	if err != nil {
		_ = primaryStore.Close(ctx)
		return nil, fmt.Errorf("telemetry: %w", err)
	}

	cacheLayer, err := newCacheHub(cfg.Redis)
	if err != nil {
		_ = telemetryPipeline.Shutdown(ctx)
		_ = primaryStore.Close(ctx)
		return nil, fmt.Errorf("cache: %w", err)
	}

	cachedStore := store.WithRedis(primaryStore, cacheLayer, store.CacheConfig{
		WorkspaceTTL: cfg.CacheTTL.workspaceOr(5 * time.Minute),
		FavoritesTTL: cfg.CacheTTL.favoritesOr(2 * time.Minute),
	})

	auth.Seed(ctx, cachedStore, cfg.AdminEmail, cfg.AdminPassword)

	jwtSvc := auth.NewJWT(cfg.JWTSecret)

	authHandler := auth.NewHandler(cachedStore, jwtSvc, auth.Config{
		FrontendURL:         cfg.FrontendURL,
		APIBaseURL:          cfg.APIBaseURL,
		PasswordAuthEnabled: cfg.PasswordAuthEnabled,
		SignUpEnabled:       cfg.SignUpEnabled,
		TrustedProxyCIDRs:   cfg.TrustedProxies,
	})

	suiteService := suites.NewService()

	agentRuntimeFile := cfg.AgentRuntimeFile
	if agentRuntimeFile == "" {
		agentRuntimeFile = "babelsuite-agents.yaml"
	}
	var agentRuntimeStore agent.RuntimeStore = agent.NewFileRuntimeStore(agentRuntimeFile)
	if repository, ok := primaryStore.(agent.RuntimeRepository); ok {
		agentRuntimeStore = agent.NewDBRuntimeStore(repository)
	}

	platformBaseStore := platform.NewFileStore(cfg.PlatformSettingsFile)
	if _, err := platformBaseStore.Load(); err != nil {
		_ = cacheLayer.Close()
		_ = telemetryPipeline.Shutdown(ctx)
		_ = primaryStore.Close(ctx)
		return nil, fmt.Errorf("platform settings: %w", err)
	}
	var platformStore platform.Store = platformBaseStore
	platformStore = platform.WithRedis(platformStore, cacheLayer, cfg.CacheTTL.platformOr(2*time.Minute))

	mockingService := mocking.NewService(suiteService)
	catalogReader := catalog.WithRedis(
		catalog.NewService(suiteService, platformStore),
		cacheLayer,
		cfg.CacheTTL.catalogOr(45*time.Second),
	)

	// catalogBacked lists suites from the OCI registry so all pages agree on
	// which suites exist. Get/Resolve prefer the workspace reader (suite.star).
	catalogBacked := &catalogSuiteReader{catalog: catalogReader, workspace: suiteService}

	profileBaseStore := profiles.NewFileStore(cfg.ProfilesFile)
	var profileStore profiles.Store = profileBaseStore
	profileStore = profiles.WithRedis(profileStore, cacheLayer, cfg.CacheTTL.profilesOr(2*time.Minute))
	profileService := profiles.NewService(catalogBacked, profileStore)

	engineStore := engine.NewStore()
	agentRegistry := agent.NewRegistry(agentRuntimeStore)
	executionWatcher := enginewatchers.NewExecutionWatcher(engineStore)
	executionService := execution.NewServiceWithPlatform(profileService, platformStore, executionWatcher)
	executionService.ConfigureMockResetter(mockingService)
	if runtimeStore, ok := primaryStore.(execution.RuntimeStore); ok {
		executionService.ConfigureRuntimeStore(runtimeStore)
	}
	executionService.ConfigureRuntimeCache(cacheLayer, cfg.CacheTTL.executionRuntimeOr(24*time.Hour))

	assignmentCoordinator := agent.NewCoordinator(agentRegistry, executionService)
	if assignmentStore, ok := primaryStore.(agent.AssignmentStore); ok {
		assignmentCoordinator.ConfigureStore(assignmentStore)
	}
	assignmentCoordinator.ConfigureRuntimeCache(cacheLayer, cfg.CacheTTL.executionRuntimeOr(24*time.Hour))
	executionService.ConfigureRemoteWorkers(agentRegistry, assignmentCoordinator)

	environmentService := environments.NewService()

	var cronService *cronjobs.Service
	if cronStore, ok := primaryStore.(cronjobs.Store); ok {
		smtpFn := func() cronjobs.SMTPConfig {
			s, err := platformBaseStore.Load()
			if err != nil || s == nil {
				return cronjobs.SMTPConfig{}
			}
			n := s.Notifications.SMTP
			return cronjobs.SMTPConfig{
				Host:     n.Host,
				Port:     n.Port,
				Username: n.Username,
				Password: n.Password,
				From:     n.From,
			}
		}
		cronService = cronjobs.NewService(cronStore, smtpFn, executionService)
	}

	health := buildHealthService("mongo", primaryStore, cacheLayer, telemetryPipeline,
		platformBaseStore, profileBaseStore, agentRegistry, executionService)

	mux := http.NewServeMux()
	health.Register(mux)
	authHandler.Register(mux)
	catalog.NewHandler(catalogReader, cachedStore, jwtSvc).Register(mux)
	engine.NewHandler(engineStore, jwtSvc).Register(mux)
	agent.RegisterGateway(mux, agentRegistry, assignmentCoordinator, cfg.AgentSharedSecret)
	profiles.NewHandler(profileService, jwtSvc).Register(mux)
	suites.NewHandler(suiteService, jwtSvc).Register(mux)
	mocking.NewHandler(mockingService, cfg.MockSharedSecret).Register(mux)
	execution.NewHandler(executionService, engineStore, jwtSvc).Register(mux)
	platform.NewHandler(platformStore, jwtSvc).Register(mux)
	environments.NewHandler(environmentService, jwtSvc).Register(mux)
	if cronService != nil {
		cronjobs.NewHandler(cronService, jwtSvc).Register(mux)
	}
	mux.Handle("POST /api/v1/telemetry/traces", auth.RequireSession(jwtSvc, auth.VerifyOptions{})(telemetry.NewTraceProxyHandler()))
	mux.Handle("POST /api/v1/telemetry/metrics", auth.RequireSession(jwtSvc, auth.VerifyOptions{})(telemetry.NewMetricsProxyHandler()))

	proxyTrust, err := httpserver.ParseProxyTrust(cfg.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}

	bgCtx, stopBackground := context.WithCancel(context.Background())
	go agentRegistry.Start(bgCtx)
	go catalog.NewRefresher(catalogReader, cfg.CacheTTL.catalogOr(45*time.Second)/2).Start(bgCtx)
	if cronService != nil {
		if err := cronService.Start(bgCtx); err != nil {
			stopBackground()
			return nil, fmt.Errorf("cron: %w", err)
		}
	}

	return &App{
		handler:            buildHandler(cfg.FrontendURL, jwtSvc, proxyTrust, mux, cachedStore),
		telemetryPipeline:  telemetryPipeline,
		cacheLayer:         cacheLayer,
		primaryStore:       primaryStore,
		executionService:   executionService,
		environmentService: environmentService,
		cronService:        cronService,
		stopBackground:     stopBackground,
	}, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

// Close releases all resources held by the App.
func (a *App) Close(ctx context.Context) error {
	if a.stopBackground != nil {
		a.stopBackground()
	}
	var combined error
	if a.cronService != nil {
		a.cronService.Stop()
	}
	if a.environmentService != nil {
		a.environmentService.Close()
	}
	if a.executionService != nil {
		a.executionService.Close()
	}
	if a.cacheLayer != nil {
		combined = errors.Join(combined, a.cacheLayer.Close())
	}
	if a.telemetryPipeline != nil {
		combined = errors.Join(combined, a.telemetryPipeline.Shutdown(ctx))
	}
	if a.primaryStore != nil {
		combined = errors.Join(combined, a.primaryStore.Close(ctx))
	}
	return combined
}

func newMongoStore(cfg Config) (store.Store, error) {
	uri := cfg.MongoURI
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	db := cfg.MongoDB
	if db == "" {
		db = "babelsuite"
	}
	return mongostore.New(uri, db)
}

func newCacheHub(rc *RedisConfig) (*cachehub.Hub, error) {
	if rc == nil || rc.Addr == "" {
		return &cachehub.Hub{}, nil
	}
	prefix := rc.Prefix
	if prefix == "" {
		prefix = "babelsuite"
	}
	return cachehub.New(cachehub.Options{
		Address:  rc.Addr,
		Password: rc.Password,
		DB:       rc.DB,
		Prefix:   prefix,
	})
}

func buildHandler(frontendURL string, jwt *auth.JWTService, trust *httpserver.ProxyTrust, mux http.Handler, auditWriter httpserver.AuditWriter) http.Handler {
	metrics := httpserver.NewHTTPMetrics()
	return httpserver.Chain(
		mux,
		httpserver.RecoveryMiddleware(),
		httpserver.SecurityHeadersMiddleware(trust),
		corsMiddleware(frontendURL),
		httpserver.CSRFMiddleware(),
		httpserver.BodyLimitMiddleware(10*1024*1024),
		httpserver.RequestIDMiddleware(),
		auth.PopulateSession(jwt, auth.VerifyOptions{}),
		telemetry.WrapHTTP,
		httpserver.TraceContextMiddleware(),
		metrics.Middleware(),
		httpserver.AuditMiddleware(auditWriter),
	)
}

func corsMiddleware(frontendURL string) httpserver.Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && frontendURL != "" {
				if origin != frontendURL {
					http.Error(w, "CORS origin not allowed", http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, Traceparent, Tracestate, Baggage, Cache-Control, Last-Event-ID")
				w.Header().Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
