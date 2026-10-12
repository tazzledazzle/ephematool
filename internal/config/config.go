// Package config loads all Phase 1 runtime configuration from the environment.
package config

import (
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
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

	CookieSecure         bool          `env:"COOKIE_SECURE" envDefault:"false"`
	SessionIdleTimeout   time.Duration `env:"SESSION_IDLE_TIMEOUT" envDefault:"168h"`
	SessionLifetime      time.Duration `env:"SESSION_LIFETIME" envDefault:"720h"`
	SessionTouchInterval time.Duration `env:"SESSION_TOUCH_INTERVAL" envDefault:"5m"`

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
