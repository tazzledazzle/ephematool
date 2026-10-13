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
	if c.PreviewPort != 8088 {
		t.Errorf("PreviewPort = %d, want 8088", c.PreviewPort)
	}
	if c.PreviewDomain != "preview.localhost" {
		t.Errorf("PreviewDomain = %q", c.PreviewDomain)
	}
	if c.EnvDefaultTTL.Hours() != 24 {
		t.Errorf("EnvDefaultTTL = %v, want 24h", c.EnvDefaultTTL)
	}
	if c.ImageTagFormat != "{sha}" {
		t.Errorf("ImageTagFormat = %q", c.ImageTagFormat)
	}
	if c.MigrationURL() != c.DatabaseURL {
		t.Errorf("MigrationURL should fall back to DatabaseURL")
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
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.PreviewURL("pv-abc1234567"), "http://pv-abc1234567.preview.localhost:8088"; got != want {
		t.Errorf("PreviewURL = %q, want %q", got, want)
	}

	t.Setenv("PREVIEW_PORT", "80")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.PreviewURL("pv-abc1234567"), "http://pv-abc1234567.preview.localhost"; got != want {
		t.Errorf("port 80 should be omitted: got %q, want %q", got, want)
	}
}

func TestGitHubAppConfigured(t *testing.T) {
	c, _ := Load()
	if c.GitHubAppConfigured() {
		t.Error("should be false with nothing set")
	}
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "2")
	c, _ = Load()
	if c.GitHubAppConfigured() {
		t.Error("should be false without the key file")
	}
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", "/k.pem")
	c, _ = Load()
	if !c.GitHubAppConfigured() {
		t.Error("should be true with all three")
	}
}
func TestLoadRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string // substring the error must contain
	}{
		{"idle timeout zero", map[string]string{"SESSION_IDLE_TIMEOUT": "0"}, "SESSION_IDLE_TIMEOUT"},
		{"probe interval zero", map[string]string{"PROBE_INTERVAL": "0"}, "PROBE_INTERVAL"},
		{"heartbeat zero", map[string]string{"HEARTBEAT_INTERVAL": "0s"}, "HEARTBEAT_INTERVAL"},
		{"env poll initial zero", map[string]string{"ENV_POLL_INITIAL": "0"}, "ENV_POLL_INITIAL"},
		{"probe timeout >= interval", map[string]string{"PROBE_TIMEOUT": "10s"}, "PROBE_TIMEOUT"},
		{"idle > lifetime", map[string]string{"SESSION_IDLE_TIMEOUT": "800h"}, "SESSION_IDLE_TIMEOUT"},
		{"heartbeat too slow", map[string]string{"HEARTBEAT_INTERVAL": "30s"}, "HEARTBEAT_INTERVAL"},
		{"poll initial > max", map[string]string{"ENV_POLL_INITIAL": "20s"}, "ENV_POLL_INITIAL"},
		{"env probe timeout >= interval", map[string]string{"ENV_PROBE_TIMEOUT": "5s"}, "ENV_PROBE_TIMEOUT"},
		{"env probe interval >= window", map[string]string{"ENV_PROBE_INTERVAL": "60s"}, "ENV_PROBE_INTERVAL"},
		{"ticket ttl too long", map[string]string{"PREVIEW_TICKET_TTL": "10m"}, "PREVIEW_TICKET_TTL"},
		{"https base url, insecure cookie", map[string]string{"PUBLIC_BASE_URL": "https://x.example", "COOKIE_SECURE": "false"}, "COOKIE_SECURE"},
		{"https preview, insecure cookie", map[string]string{"PREVIEW_SCHEME": "https", "COOKIE_SECURE": "false"}, "COOKIE_SECURE"},
		{"team without org", map[string]string{"ALLOWED_TEAMS": "sre"}, "ALLOWED_TEAMS"},
		{"bad repo", map[string]string{"ALLOWED_REPOS": "org/app, bad"}, "ALLOWED_REPOS"},
		{"environments repo no slash", map[string]string{"ENVIRONMENTS_REPO": "noslash"}, "ENVIRONMENTS_REPO"},
		{"image tag without sha", map[string]string{"IMAGE_TAG_FORMAT": "latest"}, "IMAGE_TAG_FORMAT"},
		{"port zero", map[string]string{"PREVIEW_PORT": "0"}, "PREVIEW_PORT"},
		{"bad domain", map[string]string{"PREVIEW_DOMAIN": "Bad_Domain"}, "PREVIEW_DOMAIN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %s", err, tc.want)
			}
		})
	}
}

func TestLoadAccepts(t *testing.T) {
	cases := map[string]map[string]string{
		"ping throttle zero (e2e overlay)": {"PING_THROTTLE": "0"},
		"creator team org/slug":            {"CREATOR_TEAMS": "org/team"},
		"valid repos":                      {"ALLOWED_REPOS": "org/app, org/other"},
		"https with secure cookie":         {"PREVIEW_SCHEME": "https", "COOKIE_SECURE": "true"},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			for k, v := range e {
				t.Setenv(k, v)
			}
			if _, err := Load(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
