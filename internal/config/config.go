// Package config loads all Phase 1 runtime configuration from the environment.
package config

import (
	"github.com/caarlos0/env/v11"
	"strconv"
	"strings"
	"time"
)

// Config holds every Phase 1 setting. Later plans consume fields from here
// instead of editing this file.
type Config struct {
	AppEnv           string `env:"APP_ENV" envDefault:"local"`
	HTTPAddr         string `env:"HTTP_ADDR" envDefault:":8081"`
	PublicBaseURL    string `env:"PUBLIC_BASE_URL" envDefault:"http://localhost:8080"`
	GrafanaPublicURL string `env:"GRAFANA_PUBLIC_URL" envDefault:"http://localhost:3000"`

	DatabaseURL    string `env:"DATABASE_URL" envDefault:"postgres://ephematool:ephematool@postgres:5432/ephematool?sslmode=disable"`
	MigrateOnStart bool   `env:"MIGRATE_ON_START" envDefault:"true"`
	RedisAddr      string `env:"REDIS_ADDR" envDefault:"redis:6379"`

	GitHubClientID     string `env:"GITHUB_CLIENT_ID"`
	GitHubClientSecret string `env:"GITHUB_CLIENT_SECRET"`
	GitHubAuthorizeURL string `env:"GITHUB_AUTHORIZE_URL" envDefault:"https://github.com/login/oauth/authorize"`
	GitHubTokenURL     string `env:"GITHUB_TOKEN_URL" envDefault:"https://github.com/login/oauth/access_token"`
	GitHubAPIURL       string `env:"GITHUB_API_URL" envDefault:"https://api.github.com/"`

	AllowedOrgs  []string `env:"ALLOWED_ORGS" envSeparator:","`
	AllowedTeams []string `env:"ALLOWED_TEAMS" envSeparator:","`
	AdminLogins  []string `env:"ADMIN_LOGINS" envSeparator:","`

	CookieSecure       bool          `env:"COOKIE_SECURE" envDefault:"false"`
	SessionIdleTimeout time.Duration `env:"SESSION_IDLE_TIMEOUT" envDefault:"168h"`
	SessionLifetime    time.Duration `env:"SESSION_LIFETIME" envDefault:"720h"`

	PingThrottle      time.Duration `env:"PING_THROTTLE" envDefault:"10s"`
	HeartbeatInterval time.Duration `env:"HEARTBEAT_INTERVAL" envDefault:"15s"`
	ProbeInterval     time.Duration `env:"PROBE_INTERVAL" envDefault:"10s"`
	ProbeTimeout      time.Duration `env:"PROBE_TIMEOUT" envDefault:"2s"`

	PrometheusURL      string `env:"PROMETHEUS_URL" envDefault:"http://prometheus:9090"`
	CollectorHealthURL string `env:"COLLECTOR_HEALTH_URL" envDefault:"http://otel-collector:13133/"`
	TempoReadyURL      string `env:"TEMPO_READY_URL" envDefault:"http://tempo:3200/ready"`
	LokiReadyURL       string `env:"LOKI_READY_URL" envDefault:"http://loki:3100/ready"`
	PrometheusReadyURL string `env:"PROMETHEUS_READY_URL" envDefault:"http://prometheus:9090/-/ready"`
	GrafanaHealthURL   string `env:"GRAFANA_HEALTH_URL" envDefault:"http://grafana:3000/api/health"`
	WebHealthURL       string `env:"WEB_HEALTH_URL" envDefault:"http://web:8080/healthz"`
	MigrateDatabaseURL string `env:"MIGRATE_DATABASE_URL"`

	CreatorLogins []string `env:"CREATOR_LOGINS" envSeparator:","`
	CreatorTeams  []string `env:"CREATOR_TEAMS" envSeparator:","`
	AllowedRepos  []string `env:"ALLOWED_REPOS" envSeparator:","`

	GitHubAppID             int64  `env:"GITHUB_APP_ID"`
	GitHubAppInstallationID int64  `env:"GITHUB_APP_INSTALLATION_ID"`
	GitHubAppPrivateKeyFile string `env:"GITHUB_APP_PRIVATE_KEY_FILE"`

	EnvironmentsRepo     string `env:"ENVIRONMENTS_REPO"`
	EnvironmentsWorkflow string `env:"ENVIRONMENTS_WORKFLOW" envDefault:"create-environment.yml"`
	EnvironmentsRef      string `env:"ENVIRONMENTS_REF" envDefault:"main"`
	GHCRBaseURL          string `env:"GHCR_BASE_URL" envDefault:"https://ghcr.io"`
	ImageTagFormat       string `env:"IMAGE_TAG_FORMAT" envDefault:"{sha}"`

	PreviewDomain               string        `env:"PREVIEW_DOMAIN" envDefault:"preview.localhost"`
	PreviewPort                 int           `env:"PREVIEW_PORT" envDefault:"8088"`
	PreviewScheme               string        `env:"PREVIEW_SCHEME" envDefault:"http"`
	PreviewGatewayAddr          string        `env:"PREVIEW_GATEWAY_ADDR" envDefault:"host.docker.internal:8088"`
	PreviewTokenKeyFile         string        `env:"PREVIEW_TOKEN_KEY_FILE"`
	PreviewTokenPreviousKeyFile string        `env:"PREVIEW_TOKEN_PREVIOUS_KEY_FILE"`
	PreviewTicketTTL            time.Duration `env:"PREVIEW_TICKET_TTL" envDefault:"60s"`
	PreviewCookieTTL            time.Duration `env:"PREVIEW_COOKIE_TTL" envDefault:"30m"`

	EnvDefaultTTL        time.Duration `env:"ENV_DEFAULT_TTL" envDefault:"24h"`
	EnvProvisionDeadline time.Duration `env:"ENV_PROVISION_DEADLINE" envDefault:"15m"`
	EnvPollInitial       time.Duration `env:"ENV_POLL_INITIAL" envDefault:"3s"`
	EnvPollMax           time.Duration `env:"ENV_POLL_MAX" envDefault:"15s"`
	EnvProbeEvery        time.Duration `env:"ENV_PROBE_INTERVAL" envDefault:"5s"`
	EnvProbeWindow       time.Duration `env:"ENV_PROBE_WINDOW" envDefault:"60s"`
	EnvProbeTimeout      time.Duration `env:"ENV_PROBE_TIMEOUT" envDefault:"2s"`
	EnvSweepInterval     time.Duration `env:"ENV_SWEEP_INTERVAL" envDefault:"30s"`
	EnvDispatchSpacing   time.Duration `env:"ENV_DISPATCH_SPACING" envDefault:"1s"`
}

// Load parses the environment into a Config. Comma lists are trimmed and
// empty entries dropped.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, err
	}
	c.AllowedOrgs = cleanList(c.AllowedOrgs)
	c.AllowedTeams = cleanList(c.AllowedTeams)
	c.AdminLogins = cleanList(c.AdminLogins)
	c.CreatorLogins = cleanList(c.CreatorLogins)
	c.CreatorTeams = cleanList(c.CreatorTeams)
	c.AllowedRepos = cleanList(c.AllowedRepos)

	return c, nil
}

// GitHubConfigured reports whether both OAuth credentials are present.
func (c Config) GitHubConfigured() bool {
	return c.GitHubClientID != "" && c.GitHubClientSecret != ""
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// MigrationURL is the DSN used for migrations; falls back to DatabaseURL.
func (c Config) MigrationURL() string {
	if c.MigrateDatabaseURL != "" {
		return c.MigrateDatabaseURL
	}
	return c.DatabaseURL
}

func (c Config) GitHubAppConfigured() bool {
	return c.GitHubAppID != 0 && c.GitHubAppInstallationID != 0 && c.GitHubAppPrivateKeyFile != ""
}

// PreviewHost is "<envID>.<PreviewDomain>[:<PreviewPort>]"; the port is omitted
// when it is the default for the scheme.
func (c Config) PreviewHost(envID string) string {
	host := envID + "." + c.PreviewDomain
	if (c.PreviewScheme == "http" && c.PreviewPort == 80) || (c.PreviewScheme == "https" && c.PreviewPort == 443) {
		return host
	}
	return host + ":" + strconv.Itoa(c.PreviewPort)
}

func (c Config) PreviewURL(envID string) string {
	return c.PreviewScheme + "://" + c.PreviewHost(envID)
}
