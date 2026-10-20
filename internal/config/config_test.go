package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.PreviewPort != 8088 || c.PreviewDomain != "preview.localhost" {
		t.Errorf("preview defaults = %d %q", c.PreviewPort, c.PreviewDomain)
	}
	if c.EnvDefaultTTL.Hours() != 24 {
		t.Errorf("EnvDefaultTTL = %v, want 24h", c.EnvDefaultTTL)
	}
	if c.ImageTagFormat != "{sha}" {
		t.Errorf("ImageTagFormat = %q", c.ImageTagFormat)
	}
	if c.MigrationURL() != c.DatabaseURL {
		t.Error("MigrationURL should fall back to DatabaseURL")
	}
	if c.GitHubAppConfigured() {
		t.Error("GitHubAppConfigured must be false by default")
	}
}

func TestMigrationURLOverride(t *testing.T) {
	t.Setenv("MIGRATE_DATABASE_URL", "postgres://owner@db/x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.MigrationURL(); got != "postgres://owner@db/x" {
		t.Errorf("MigrationURL = %q", got)
	}
}

func TestPreviewURL(t *testing.T) {
	c, _ := Load()
	if got, want := c.PreviewURL("pv-abc1234567"), "http://pv-abc1234567.preview.localhost:8088"; got != want {
		t.Errorf("PreviewURL = %q, want %q", got, want)
	}
	t.Setenv("PREVIEW_PORT", "80") // default port for http: omitted from the host
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.PreviewHost("pv-abc1234567"), "pv-abc1234567.preview.localhost"; got != want {
		t.Errorf("PreviewHost = %q, want %q", got, want)
	}
}

func TestGitHubAppConfigured(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "2")
	c, _ := Load()
	if c.GitHubAppConfigured() {
		t.Error("needs the key file too")
	}
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", "/run/x.pem")
	c, _ = Load()
	if !c.GitHubAppConfigured() {
		t.Error("all three set, want configured")
	}
}

func TestPingThrottleZeroAllowed(t *testing.T) {
	t.Setenv("PING_THROTTLE", "0s")
	if _, err := Load(); err != nil {
		t.Fatalf("PING_THROTTLE=0 must load (e2e overlay): %v", err)
	}
}

// Every case must fail Load() with an error that names the variable (or the
// phrase in want), so an operator can fix the right setting.
func TestLoadRejects(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"zero idle timeout", map[string]string{"SESSION_IDLE_TIMEOUT": "0"}, "SESSION_IDLE_TIMEOUT"},
		{"zero probe interval", map[string]string{"PROBE_INTERVAL": "0"}, "PROBE_INTERVAL"},
		{"zero heartbeat", map[string]string{"HEARTBEAT_INTERVAL": "0s"}, "HEARTBEAT_INTERVAL"},
		{"zero poll initial", map[string]string{"ENV_POLL_INITIAL": "0"}, "ENV_POLL_INITIAL"},
		{"zero probe timeout", map[string]string{"PROBE_TIMEOUT": "0"}, "PROBE_TIMEOUT"},
		{"probe timeout >= interval", map[string]string{"PROBE_TIMEOUT": "10s", "PROBE_INTERVAL": "10s"}, "PROBE_TIMEOUT"},
		{"idle > lifetime", map[string]string{"SESSION_IDLE_TIMEOUT": "800h", "SESSION_LIFETIME": "720h"}, "SESSION_IDLE_TIMEOUT"},
		{"heartbeat too slow", map[string]string{"HEARTBEAT_INTERVAL": "30s"}, "HEARTBEAT_INTERVAL"},
		{"poll initial > max", map[string]string{"ENV_POLL_INITIAL": "20s", "ENV_POLL_MAX": "15s"}, "ENV_POLL_INITIAL"},
		{"env probe timeout >= interval", map[string]string{"ENV_PROBE_TIMEOUT": "5s", "ENV_PROBE_INTERVAL": "5s"}, "ENV_PROBE_TIMEOUT"},
		{"env probe interval >= window", map[string]string{"ENV_PROBE_INTERVAL": "60s", "ENV_PROBE_WINDOW": "60s"}, "ENV_PROBE_INTERVAL"},
		{"ticket ttl too long", map[string]string{"PREVIEW_TICKET_TTL": "10m"}, "PREVIEW_TICKET_TTL"},
		{"https base url without secure cookie", map[string]string{"PUBLIC_BASE_URL": "https://x.example", "COOKIE_SECURE": "false"}, "COOKIE_SECURE"},
		{"https preview without secure cookie", map[string]string{"PREVIEW_SCHEME": "https", "COOKIE_SECURE": "false"}, "COOKIE_SECURE"},
		{"allowed team without org", map[string]string{"ALLOWED_TEAMS": "sre"}, "ALLOWED_TEAMS"},
		{"creator team without org", map[string]string{"CREATOR_TEAMS": "creators"}, "CREATOR_TEAMS"},
		{"bad allowed repo", map[string]string{"ALLOWED_REPOS": "org/app, bad"}, "ALLOWED_REPOS"},
		{"bad environments repo", map[string]string{"ENVIRONMENTS_REPO": "noslash"}, "ENVIRONMENTS_REPO"},
		{"image tag without sha", map[string]string{"IMAGE_TAG_FORMAT": "latest"}, "IMAGE_TAG_FORMAT"},
		{"port zero", map[string]string{"PREVIEW_PORT": "0"}, "PREVIEW_PORT"},
		{"port too big", map[string]string{"PREVIEW_PORT": "70000"}, "PREVIEW_PORT"},
		{"bad scheme", map[string]string{"PREVIEW_SCHEME": "ftp"}, "PREVIEW_SCHEME"},
		{"bad domain", map[string]string{"PREVIEW_DOMAIN": "Bad_Domain"}, "PREVIEW_DOMAIN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadAccepts(t *testing.T) {
	t.Setenv("CREATOR_TEAMS", "org/team")
	t.Setenv("ALLOWED_REPOS", "org/app, org/other")
	t.Setenv("ENVIRONMENTS_REPO", "org/ephematool-environments")
	t.Setenv("IMAGE_TAG_FORMAT", "{sha7}")
	t.Setenv("PUBLIC_BASE_URL", "https://dash.example")
	t.Setenv("COOKIE_SECURE", "true")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.AllowedRepos) != 2 || c.AllowedRepos[1] != "org/other" {
		t.Errorf("AllowedRepos = %v (lists are trimmed)", c.AllowedRepos)
	}
}
