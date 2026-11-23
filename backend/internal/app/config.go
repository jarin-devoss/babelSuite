package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/auth"
)

// SMTPConfig holds optional SMTP settings for cron job email notifications.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Config holds all configuration needed to start the application.
// Zero values are valid where noted; the New function applies defaults.
type Config struct {
	JWTSecret     string
	AdminEmail    string
	AdminPassword string

	// DBDriver is the datastore: "mongo" (the default) or "postgres".
	DBDriver    string
	PostgresURL string // used by "postgres"
	MongoURI    string
	MongoDB     string

	PlatformSettingsFile string
	ProfilesFile         string
	AgentRuntimeFile     string // optional; defaults to "babelsuite-agents.yaml" relative to cwd

	// FrontendURL is the CORS allowed origin. Empty string skips the CORS check.
	FrontendURL string
	APIBaseURL  string

	// TrustedProxies is a list of CIDR ranges whose X-Forwarded-* headers are trusted.
	// Empty means no proxy is trusted (safe default).
	TrustedProxies []string

	PasswordAuthEnabled bool
	SignUpEnabled       bool

	// OIDC is single sign-on, read from the OIDC_* variables. The zero value is off.
	OIDC auth.OIDCConfig

	MockSharedSecret  string
	AgentSharedSecret string

	Redis    *RedisConfig
	CacheTTL CacheTTLConfig
	SMTP     SMTPConfig
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	Prefix   string
}

type CacheTTLConfig struct {
	Workspace        time.Duration
	Favorites        time.Duration
	Profiles         time.Duration
	Platform         time.Duration
	Catalog          time.Duration
	ExecutionRuntime time.Duration
}

func (c CacheTTLConfig) workspaceOr(d time.Duration) time.Duration {
	if c.Workspace > 0 {
		return c.Workspace
	}
	return d
}

func (c CacheTTLConfig) favoritesOr(d time.Duration) time.Duration {
	if c.Favorites > 0 {
		return c.Favorites
	}
	return d
}

func (c CacheTTLConfig) profilesOr(d time.Duration) time.Duration {
	if c.Profiles > 0 {
		return c.Profiles
	}
	return d
}

func (c CacheTTLConfig) platformOr(d time.Duration) time.Duration {
	if c.Platform > 0 {
		return c.Platform
	}
	return d
}

func (c CacheTTLConfig) catalogOr(d time.Duration) time.Duration {
	if c.Catalog > 0 {
		return c.Catalog
	}
	return d
}

func (c CacheTTLConfig) executionRuntimeOr(d time.Duration) time.Duration {
	if c.ExecutionRuntime > 0 {
		return c.ExecutionRuntime
	}
	return d
}

// The datastores the server can run on.
const (
	DriverMongo    = "mongo"
	DriverPostgres = "postgres"
)

// databaseDriver is the datastore the configuration selects. An unknown driver,
// or postgres without a connection string, is an error at startup rather than a
// silent fall back to MongoDB: the operator asked for a particular datastore.
func (c Config) databaseDriver() (string, error) {
	driver := strings.ToLower(strings.TrimSpace(c.DBDriver))
	switch driver {
	case "", DriverMongo:
		return DriverMongo, nil
	case DriverPostgres:
		if strings.TrimSpace(c.PostgresURL) == "" {
			return "", fmt.Errorf("DB_DRIVER=postgres needs POSTGRES_URL (for example postgres://user:password@host:5432/babelsuite)")
		}
		return DriverPostgres, nil
	}
	return "", fmt.Errorf("DB_DRIVER %q is not supported: use %q or %q", c.DBDriver, DriverMongo, DriverPostgres)
}
