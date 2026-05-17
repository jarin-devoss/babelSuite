package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/babelsuite/babelsuite/internal/app"
)

func newApp(ctx context.Context) (*app.App, error) {
	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if jwtSecret == "" || jwtSecret == "change-me" {
		return nil, errorf("JWT_SECRET must be set to a secure random value")
	}

	frontendURL := envOr("FRONTEND_URL", "http://localhost:5173")
	apiBaseURL := envOr("PUBLIC_API_URL", envOr("VITE_API_URL", "http://localhost:"+envOr("PORT", "8090")))

	cfg := app.Config{
		JWTSecret:     jwtSecret,
		AdminEmail:    strings.TrimSpace(os.Getenv("ADMIN_EMAIL")),
		AdminPassword: strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")),

		MongoURI: envOr("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:  envOr("MONGO_DB", "babelsuite"),

		PlatformSettingsFile: resolveWorkspacePath(envOr("PLATFORM_SETTINGS_FILE", "configuration.yaml")),
		ProfilesFile:         resolveWorkspacePath(envOr("PROFILES_FILE", "babelsuite-profiles.yaml")),
		AgentRuntimeFile:     resolveWorkspacePath(envOr("AGENT_RUNTIME_FILE", "babelsuite-agents.yaml")),

		FrontendURL:         frontendURL,
		APIBaseURL:          apiBaseURL,
		PasswordAuthEnabled: boolEnv("AUTH_PASSWORD_LOGIN_ENABLED", true),
		SignUpEnabled:       boolEnv("AUTH_SIGNUP_ENABLED", true),

		MockSharedSecret:  strings.TrimSpace(os.Getenv("MOCK_SHARED_SECRET")),
		AgentSharedSecret: strings.TrimSpace(os.Getenv("AGENT_SHARED_SECRET")),
		TrustedProxies:    splitCSV(os.Getenv("TRUSTED_PROXIES")),

		Redis: redisConfig(),

		CacheTTL: app.CacheTTLConfig{
			Workspace:        durationOr("CACHE_TTL_WORKSPACE", 0),
			Favorites:        durationOr("CACHE_TTL_FAVORITES", 0),
			Profiles:         durationOr("CACHE_TTL_PROFILES", 0),
			Platform:         durationOr("CACHE_TTL_PLATFORM", 0),
			Catalog:          durationOr("CACHE_TTL_CATALOG", 0),
			ExecutionRuntime: durationOr("CACHE_TTL_EXECUTION_RUNTIME", 0),
		},
	}

	return app.New(ctx, cfg)
}

func redisConfig() *app.RedisConfig {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		return nil
	}
	return &app.RedisConfig{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       intEnv("REDIS_DB", 0),
		Prefix:   envOr("REDIS_PREFIX", "babelsuite"),
	}
}

func errorf(format string, args ...any) error {
	return &fmtError{msg: fmt.Sprintf(format, args...)}
}

type fmtError struct{ msg string }

func (e *fmtError) Error() string { return e.msg }
