package config

import "testing"

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
