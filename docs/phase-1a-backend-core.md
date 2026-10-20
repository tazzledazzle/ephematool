# Phase 1a · Backend core: config, telemetry, store, queue, status, auth, API

**Objective:** build the Go backend of the telemetry spine: configuration, OpenTelemetry bootstrap, Postgres store, asynq queue with trace propagation, the service status prober, GitHub OAuth login (PKCE, org/team allowlist, Redis sessions), a fake GitHub for tests, the Huma HTTP API, and the two binaries (`api`, `worker`). Everything has unit tests; the login flow also has a Docker-backed integration test.

**Design references:** `.planning/phases/01-telemetry-spine-github-login/01-CONTEXT.md` (decisions D-01..D-15), `01-RESEARCH.md` (pitfalls 1..11), `01-UI-SPEC.md` (API shapes the SPA consumes).
**Requirements covered:** OBS-01 (backend half), AUTH-01.
**Prerequisite:** Phase 0 acceptance checklist complete.
**Produces (later phases rely on these exact names):**

| Package | Public API used later |
|---|---|
| `internal/config` | `Config`, `Load()`, `Config.GitHubConfigured()` (Phase 2 appends fields; see Phase 2a) |
| `internal/telemetry` | `Setup(ctx, Config) (shutdown, error)`, PromQL helpers |
| `internal/store` | `Open`, `OpenUntraced`, `Migrate`, `New(db) *Queries`, `UpsertUser`, `GetUserByGithubID` |
| `internal/queue` | `Client`, `InjectHeaders`, `TracingMiddleware`, `NewServer`, `NewMux`, `NewScheduler`, `HeartbeatKey` |
| `internal/status` | `Prober`, `Checks`, `Services` |
| `internal/auth` | `NewSessionManager`, `NewHandlers`, `Register`, `RequireSession`, `Allowlist`, `RoleFor`, `SafeNext`, session keys |
| `internal/httpapi` | `New(Deps) (http.Handler, huma.API)`, `RouteTagging`, `RouteHandler`, `TraceIDHeader` |
| `internal/conventions` | `K8sLabelPrefix`, `OTelAttrPrefix`, `Label*`, `Attr*` constants |
| `internal/fakegithub` | `New()`, `Fixtures()` (extended in Phase 2a) |

---

## 1. Directory structure after this phase

```
cmd/
├── api/{BUILD.bazel,main.go}
├── worker/{BUILD.bazel,main.go}
└── fakegithub/{BUILD.bazel,main.go}
api/{BUILD.bazel,openapi.json}                 # openapi.json is generated
conventions/{BUILD.bazel,telemetry.json}
sqlc.yaml
tools/genconventions/{BUILD.bazel,main.go,main_test.go}
internal/
├── buildinfo/buildinfo.go
├── config/{config.go,validate.go,config_test.go}
├── conventions/{generate.go,conventions.gen.go,conventions_test.go}
├── telemetry/{telemetry.go,promql.go}
├── store/
│   ├── {store.go,generate.go,db.go,models.go,users.sql.go}
│   ├── migrations/00001_users.sql
│   └── queries/users.sql
├── queue/{tasks.go,client.go,server.go,heartbeat.go,middleware.go,middleware_test.go}
├── status/{model.go,prober.go,lastsignal.go,prober_test.go}
├── fakegithub/{server.go,server_test.go}
├── auth/
│   ├── {allowlist.go,handlers.go,middleware.go,next.go,oauth.go,role.go,session.go}
│   ├── {allowlist_test.go,next_test.go,session_test.go,oauth_integration_test.go}
│   └── authtest/env.go
└── httpapi/{api.go,middleware.go,status.go,middleware_test.go}
```

```bash
mkdir -p cmd/api cmd/worker cmd/fakegithub api conventions tools/genconventions
mkdir -p internal/{buildinfo,config,conventions,telemetry,queue,status,fakegithub,httpapi}
mkdir -p internal/store/{migrations,queries} internal/auth/authtest
```

> **How BUILD files work here.** Almost every `BUILD.bazel` is generated. After you add Go files, run `bazel run //:gazelle`. Five BUILD files are hand-written because they carry code generation rules; they are listed in section 12 and you must write them yourself. If a test needs a dependency Gazelle cannot guess, the error message names the missing label.

---

## 2. `internal/buildinfo`

Version metadata injected at link time by the Dockerfile (`-ldflags -X`). The API reports it on `/api/status`; the web app reports its own copy.

`internal/buildinfo/buildinfo.go`

```go
// Package buildinfo exposes version metadata injected with -ldflags -X.
package buildinfo

// Set at link time by the Dockerfile.
var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

// BuildInfo is the wire shape of one component's build metadata.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}

// Info returns the build metadata of this binary.
func Info() BuildInfo {
	return BuildInfo{Version: Version, Commit: Commit, BuiltAt: BuiltAt}
}
```
---

## 3. `internal/config`

All runtime configuration comes from environment variables, parsed once, with defaults that make `bazel run //:up` work with no `.env`. Validation names the offending variable so a bad deploy fails fast with a useful message.

> This is the Phase 1 subset. Phase 2a (plan 02-02) appends the environment-lifecycle fields and validation rules to these same two files.

`internal/config/config.go`

```go
// Package config loads all runtime configuration from the environment.
package config

import (
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds every setting. Later phases append fields here instead of
// reading os.Getenv directly.
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
	SessionIdleTimeout time.Duration `env:"SESSION_IDLE_TIMEOUT" envDefault:"168h"` // sliding 7 days (D-03)
	SessionLifetime    time.Duration `env:"SESSION_LIFETIME" envDefault:"720h"`     // absolute 30 days (D-14)

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
// empty entries dropped. It returns the first validation error.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, err
	}
	c.AllowedOrgs = cleanList(c.AllowedOrgs)
	c.AllowedTeams = cleanList(c.AllowedTeams)
	c.AdminLogins = cleanList(c.AdminLogins)
	if err := validate(c); err != nil {
		return Config{}, err
	}
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
```

`internal/config/validate.go`

```go
package config

import (
	"fmt"
	"net/url"
	"time"
)

type durationCheck struct {
	envName string
	value   time.Duration
}

// heartbeatMax is half the 45s heartbeat TTL in internal/queue. A heartbeat slower than this
// can miss its TTL window and make a healthy worker look down.
const heartbeatMax = 22500 * time.Millisecond

// validate returns the first violated rule, always naming the env variable.
func validate(c Config) error {
	// Rule group 1: durations must be positive (PING_THROTTLE is allowed to be 0).
	// Checked first so a zero value is reported by name instead of tripping a relation below.
	for _, ch := range []durationCheck{
		{"SESSION_IDLE_TIMEOUT", c.SessionIdleTimeout},
		{"SESSION_LIFETIME", c.SessionLifetime},
		{"HEARTBEAT_INTERVAL", c.HeartbeatInterval},
		{"PROBE_INTERVAL", c.ProbeInterval},
		{"PROBE_TIMEOUT", c.ProbeTimeout},
	} {
		if ch.value <= 0 {
			return fmt.Errorf("%s must be > 0", ch.envName)
		}
	}

	// Rule group 2: relationships between durations.
	switch {
	case c.SessionIdleTimeout > c.SessionLifetime:
		return fmt.Errorf("SESSION_IDLE_TIMEOUT must be <= SESSION_LIFETIME")
	case c.ProbeTimeout >= c.ProbeInterval:
		return fmt.Errorf("PROBE_TIMEOUT must be < PROBE_INTERVAL")
	case c.HeartbeatInterval >= heartbeatMax:
		return fmt.Errorf("HEARTBEAT_INTERVAL must be < %s (half the heartbeat TTL)", heartbeatMax)
	}

	// Rule group 3 (Pitfall 11): a session cookie sent over HTTPS must be Secure.
	if u, err := url.Parse(c.PublicBaseURL); err == nil && u.Scheme == "https" && !c.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must be true when PUBLIC_BASE_URL is https")
	}
	return nil
}
```

`internal/config/config_test.go`

```go
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
	if c.HTTPAddr != ":8081" {
		t.Errorf("HTTPAddr = %q", c.HTTPAddr)
	}
	if c.SessionIdleTimeout.Hours() != 168 {
		t.Errorf("SessionIdleTimeout = %v, want 168h", c.SessionIdleTimeout)
	}
	if c.GitHubConfigured() {
		t.Error("GitHubConfigured should be false with no credentials")
	}
}

func TestListsAreTrimmed(t *testing.T) {
	t.Setenv("ALLOWED_ORGS", " a , ,b ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AllowedOrgs) != 2 || c.AllowedOrgs[0] != "a" || c.AllowedOrgs[1] != "b" {
		t.Errorf("AllowedOrgs = %q", c.AllowedOrgs)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string // substring the error must contain
	}{
		{"idle timeout zero", map[string]string{"SESSION_IDLE_TIMEOUT": "0"}, "SESSION_IDLE_TIMEOUT"},
		{"lifetime zero", map[string]string{"SESSION_LIFETIME": "0"}, "SESSION_LIFETIME"},
		{"probe interval zero", map[string]string{"PROBE_INTERVAL": "0"}, "PROBE_INTERVAL"},
		{"probe timeout zero", map[string]string{"PROBE_TIMEOUT": "0"}, "PROBE_TIMEOUT"},
		{"heartbeat zero", map[string]string{"HEARTBEAT_INTERVAL": "0s"}, "HEARTBEAT_INTERVAL"},
		{"probe timeout >= interval", map[string]string{"PROBE_TIMEOUT": "10s"}, "PROBE_TIMEOUT"},
		{"idle > lifetime", map[string]string{"SESSION_IDLE_TIMEOUT": "800h"}, "SESSION_IDLE_TIMEOUT"},
		{"heartbeat too slow", map[string]string{"HEARTBEAT_INTERVAL": "30s"}, "HEARTBEAT_INTERVAL"},
		{"https base url, insecure cookie", map[string]string{"PUBLIC_BASE_URL": "https://x.example", "COOKIE_SECURE": "false"}, "COOKIE_SECURE"},
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

func TestLoadAcceptsZeroPingThrottle(t *testing.T) {
	t.Setenv("PING_THROTTLE", "0") // the e2e overlay disables the throttle
	if _, err := Load(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

---

## 4. `internal/conventions` and the generator

Decision D-09 fixes the Kubernetes label prefix (`ephematool.io/`) and the OTel attribute prefix (`ephematool.env.`). They live in **one JSON file** and are generated into Go and TypeScript, so no hand-written prefix can drift (a later `verify` check greps for stragglers).

`conventions/telemetry.json`

```json
{
  "$comment": "Single source of truth for D-09 prefixes. K8s labels are k8sLabelPrefix + key, except envId whose label suffix is env-id. OTel attributes are otelAttrPrefix + key, except envId whose suffix is id; managed is label-only. Regenerate with: go generate ./internal/conventions/",
  "k8sLabelPrefix": "ephematool.io/",
  "otelAttrPrefix": "ephematool.env.",
  "keys": {
    "envId": "id",
    "owner": "owner",
    "template": "template",
    "expiresAt": "expires-at",
    "managed": "managed"
  }
}
```
`tools/genconventions/main.go`

```go
// Command genconventions renders conventions/telemetry.json into Go and
// TypeScript constants, and an optional shell-sourceable env file, so the D-09
// prefixes are defined exactly once.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"
)

const header = "Code generated by genconventions. DO NOT EDIT."

type spec struct {
	K8sLabelPrefix string            `json:"k8sLabelPrefix"`
	OTelAttrPrefix string            `json:"otelAttrPrefix"`
	Keys           map[string]string `json:"keys"`
}

func main() {
	in := flag.String("in", "", "input JSON")
	goOut := flag.String("go-out", "", "Go output file")
	tsOut := flag.String("ts-out", "", "TypeScript output file")
	envOut := flag.String("env-out", "", "optional shell-sourceable env output file")
	flag.Parse()
	if err := run(*in, *goOut, *tsOut, *envOut); err != nil {
		fmt.Fprintln(os.Stderr, "genconventions:", err)
		os.Exit(1)
	}
}

func run(in, goOut, tsOut, envOut string) error {
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	g, t, e, err := generate(raw)
	if err != nil {
		return err
	}
	if err := os.WriteFile(goOut, g, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(tsOut, t, 0o644); err != nil {
		return err
	}
	if envOut == "" {
		return nil
	}
	return os.WriteFile(envOut, e, 0o644)
}

// labelSuffix maps the "id" key to "env-id" (D-09); other keys are used as-is.
func labelSuffix(key, val string) string {
	if key == "envId" {
		return "env-id"
	}
	return val
}

// goName turns a JSON key like envId into EnvID.
func goName(key string) string {
	n := strings.ToUpper(key[:1]) + key[1:]
	if strings.HasSuffix(n, "Id") {
		n = n[:len(n)-2] + "ID"
	}
	return n
}

// upperSnake turns a JSON key like expiresAt into EXPIRES_AT.
func upperSnake(key string) string {
	var b strings.Builder
	for i, r := range key {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// hasAttr reports whether a key also exists as an OTel attribute; managed is label-only.
func hasAttr(key string) bool { return key != "managed" }

func generate(raw []byte) (goSrc, tsSrc, envSrc []byte, err error) {
	var s spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, nil, nil, err
	}
	if !strings.HasSuffix(s.K8sLabelPrefix, "/") {
		return nil, nil, nil, fmt.Errorf("k8sLabelPrefix %q must end in /", s.K8sLabelPrefix)
	}
	if !strings.HasSuffix(s.OTelAttrPrefix, ".") {
		return nil, nil, nil, fmt.Errorf("otelAttrPrefix %q must end in .", s.OTelAttrPrefix)
	}
	if len(s.Keys) == 0 {
		return nil, nil, nil, fmt.Errorf("keys must not be empty")
	}
	keys := make([]string, 0, len(s.Keys))
	for k := range s.Keys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var g bytes.Buffer
	fmt.Fprintf(&g, "// %s\n\n", header)
	g.WriteString("package conventions\n\n")
	fmt.Fprintf(&g, "const (\n\tK8sLabelPrefix = %q\n\tOTelAttrPrefix = %q\n)\n\n", s.K8sLabelPrefix, s.OTelAttrPrefix)
	g.WriteString("// Kubernetes label keys.\nconst (\n")
	for _, k := range keys {
		fmt.Fprintf(&g, "\tLabel%s = %q\n", goName(k), s.K8sLabelPrefix+labelSuffix(k, s.Keys[k]))
	}
	g.WriteString(")\n\n// OpenTelemetry attribute keys.\nconst (\n")
	for _, k := range keys {
		if hasAttr(k) {
			fmt.Fprintf(&g, "\tAttr%s = %q\n", goName(k), s.OTelAttrPrefix+s.Keys[k])
		}
	}
	g.WriteString(")\n")
	goSrc, err = format.Source(g.Bytes())
	if err != nil {
		return nil, nil, nil, err
	}

	var t bytes.Buffer
	fmt.Fprintf(&t, "// %s\n\n", header)
	fmt.Fprintf(&t, "export const K8S_LABEL_PREFIX = %q as const;\nexport const OTEL_ATTR_PREFIX = %q as const;\n\n", s.K8sLabelPrefix, s.OTelAttrPrefix)
	t.WriteString("export const LABELS = {\n")
	for _, k := range keys {
		fmt.Fprintf(&t, "  %s: %q,\n", k, s.K8sLabelPrefix+labelSuffix(k, s.Keys[k]))
	}
	t.WriteString("} as const;\n\nexport const ATTRS = {\n")
	for _, k := range keys {
		if hasAttr(k) {
			fmt.Fprintf(&t, "  %s: %q,\n", k, s.OTelAttrPrefix+s.Keys[k])
		}
	}
	t.WriteString("} as const;\n")

	// Env file: plain KEY=value lines so `set -a; . file` exports them (shell, Helm, collector).
	var e bytes.Buffer
	fmt.Fprintf(&e, "# %s\n", header)
	for _, k := range keys {
		fmt.Fprintf(&e, "EPHEMATOOL_LABEL_%s=%s\n", upperSnake(k), s.K8sLabelPrefix+labelSuffix(k, s.Keys[k]))
	}
	for _, k := range keys {
		if hasAttr(k) {
			fmt.Fprintf(&e, "EPHEMATOOL_ATTR_%s=%s\n", upperSnake(k), s.OTelAttrPrefix+s.Keys[k])
		}
	}
	return goSrc, t.Bytes(), e.Bytes(), nil
}
```
`tools/genconventions/main_test.go`

```go
package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = `{
  "k8sLabelPrefix": "x.io/",
  "otelAttrPrefix": "x.env.",
  "keys": {"envId": "id", "owner": "owner", "template": "template", "expiresAt": "expires-at", "managed": "managed"}
}`

func TestGenerateDeterministic(t *testing.T) {
	g1, t1, e1, err := generate([]byte(sample))
	require.NoError(t, err)
	g2, t2, e2, err := generate([]byte(sample))
	require.NoError(t, err)
	assert.Equal(t, g1, g2)
	assert.Equal(t, t1, t2)
	assert.Equal(t, e1, e2)
	assert.Contains(t, string(g1), "Code generated by genconventions. DO NOT EDIT.")
	assert.Regexp(t, `LabelEnvID\s+= "x.io/env-id"`, string(g1))
	assert.Regexp(t, `AttrEnvID\s+= "x.env.id"`, string(g1))
	assert.Contains(t, string(t1), "K8S_LABEL_PREFIX")

	// keys are emitted in sorted order
	s := string(g1)
	order := []string{"LabelEnvID", "LabelExpiresAt", "LabelManaged", "LabelOwner", "LabelTemplate"}
	last := -1
	for _, name := range order {
		i := indexOf(s, name+" ")
		require.Greater(t, i, last, name)
		last = i
	}
}

func TestEnvOutput(t *testing.T) {
	_, _, env, err := generate([]byte(sample))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(env)), "\n")
	require.Equal(t, "# Code generated by genconventions. DO NOT EDIT.", lines[0])
	assert.Equal(t, []string{
		"EPHEMATOOL_LABEL_ENV_ID=x.io/env-id",
		"EPHEMATOOL_LABEL_EXPIRES_AT=x.io/expires-at",
		"EPHEMATOOL_LABEL_MANAGED=x.io/managed",
		"EPHEMATOOL_LABEL_OWNER=x.io/owner",
		"EPHEMATOOL_LABEL_TEMPLATE=x.io/template",
		"EPHEMATOOL_ATTR_ENV_ID=x.env.id",
		"EPHEMATOOL_ATTR_EXPIRES_AT=x.env.expires-at",
		"EPHEMATOOL_ATTR_OWNER=x.env.owner",
		"EPHEMATOOL_ATTR_TEMPLATE=x.env.template",
	}, lines[1:])
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRejectsBadPrefix(t *testing.T) {
	_, _, _, err := generate([]byte(`{"k8sLabelPrefix":"x.io","otelAttrPrefix":"x.env.","keys":{"envId":"id"}}`))
	assert.Error(t, err)
	_, _, _, err = generate([]byte(`{"k8sLabelPrefix":"x.io/","otelAttrPrefix":"x.env","keys":{"envId":"id"}}`))
	assert.Error(t, err)
}
```
`internal/conventions/generate.go`

```go
// Package conventions exposes the D-09 Kubernetes label and OpenTelemetry
// attribute names, generated from conventions/telemetry.json.
package conventions

//go:generate go run ../../tools/genconventions -in ../../conventions/telemetry.json -go-out conventions.gen.go -ts-out ../../web/src/gen/conventions.gen.ts
```
`internal/conventions/conventions_test.go`

```go
package conventions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConventions(t *testing.T) {
	assert.Equal(t, "ephematool.io/", K8sLabelPrefix)
	assert.Equal(t, "ephematool.env.", OTelAttrPrefix)
	assert.Equal(t, "ephematool.io/env-id", LabelEnvID)
	assert.Equal(t, "ephematool.env.id", AttrEnvID)
}
```
Generate the Go constants (Bazel does this for you in section 12; the first time, run the generator directly):

```bash
go run ./tools/genconventions -in conventions/telemetry.json \
  -go-out internal/conventions/conventions.gen.go \
  -ts-out web/src/gen/conventions.gen.ts
```

`web/` does not exist yet, so create the folder first: `mkdir -p web/src/gen`.

Expected output of `internal/conventions/conventions.gen.go`:

`internal/conventions/conventions.gen.go`

```go
// Code generated by genconventions. DO NOT EDIT.

package conventions

const (
	K8sLabelPrefix = "ephematool.io/"
	OTelAttrPrefix = "ephematool.env."
)

// Kubernetes label keys.
const (
	LabelEnvID     = "ephematool.io/env-id"
	LabelExpiresAt = "ephematool.io/expires-at"
	LabelManaged   = "ephematool.io/managed"
	LabelOwner     = "ephematool.io/owner"
	LabelTemplate  = "ephematool.io/template"
)

// OpenTelemetry attribute keys.
const (
	AttrEnvID     = "ephematool.env.id"
	AttrExpiresAt = "ephematool.env.expires-at"
	AttrOwner     = "ephematool.env.owner"
	AttrTemplate  = "ephematool.env.template"
)
```
---

## 5. `internal/telemetry`

One call per binary sets up the three OpenTelemetry signals (traces, metrics, logs), the W3C propagator, Go runtime metrics, and routes `slog` to both stdout (JSON) and OTLP, so every log line carries the active trace id. Endpoints come from the standard `OTEL_*` environment variables, which compose sets.

`internal/telemetry/telemetry.go`

```go
// Package telemetry bootstraps OpenTelemetry traces, metrics and logs once per binary.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Config identifies the emitting service.
type Config struct {
	ServiceName string
	Version     string
	Env         string
}

// Setup registers global tracer, meter and logger providers plus the W3C
// propagator, routes the default slog logger to stdout and OTLP, and returns a
// shutdown function that flushes all three signals. Endpoints come from the
// standard OTEL_* environment variables.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceNamespace("ephematool"),
		semconv.ServiceVersion(cfg.Version),
		semconv.DeploymentEnvironmentName(cfg.Env),
	))
	if err != nil {
		return nil, err
	}

	te, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(te))

	me, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	// PeriodicReader interval honours OTEL_METRIC_EXPORT_INTERVAL.
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me)))

	le, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, err
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(le)))

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	global.SetLoggerProvider(lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	if err := runtime.Start(); err != nil {
		return nil, err
	}

	slog.SetDefault(slog.New(slog.NewMultiHandler(
		slog.NewJSONHandler(os.Stdout, nil),
		otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp)),
	)))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
	}, nil
}
```
`promql.go` holds the metric and label names the status page queries from Prometheus. Keeping them in one file means a Collector rename changes one place.

`internal/telemetry/promql.go`

```go
package telemetry

// Prometheus names observed in the Wave 0 spike (Pitfall 7). Prometheus
// translates OTLP names with UnderscoreEscapingWithSuffixes, so dotted OTel
// names never appear here. Keep every dashboard and status query in this file.
const (
	// MetricHTTPServerDurationCount is the otelhttp server duration histogram count.
	MetricHTTPServerDurationCount = "http_server_request_duration_seconds_count"
	// MetricGoRoutines is emitted by the Go runtime instrumentation of every Go
	// service (api, worker) and carries the promoted service_* labels.
	MetricGoRoutines = "go_goroutine_count"

	LabelServiceName      = "service_name"
	LabelServiceNamespace = "service_namespace"
	LabelHTTPRoute        = "http_route"
)

// LastSignalQuery returns, per service_name, the unix time of the most recent
// sample of a Go service. Spike result for assumption A4: the original form
// max by (service_name)(timestamp(target_info{service_namespace="ephematool"}))
// is EMPTY, because Prometheus' target_info series carries only job and
// telemetry.sdk labels, not the promoted service_* attributes. The promoted
// attributes live on every metric series, so the query targets one metric that
// each Go service always emits. Do not use a __name__ regex: series from
// different metrics collapse to the same labelset and the query errors.
// Non-Go services (the browser dashboard) need their own selector in plan 01-06.
const LastSignalQuery = `max by (service_name) (timestamp(go_goroutine_count{service_namespace="ephematool"}))`

// DashboardLastSignalQuery is the browser dashboard's counterpart of
// LastSignalQuery. The browser emits no Go runtime metrics, so it uses the
// page-view counter verified in plan 01-05. The browser exports every 15s, but
// only while a page is open.
const DashboardLastSignalQuery = `max by (service_name) (timestamp(ephematool_ui_page_views_total{service_name="ephematool-dashboard"}))`
```
---

## 6. `internal/store`

Postgres through `pgx`, migrations through embedded `goose`, queries through `sqlc`. Two pools exist on purpose: `Open` is traced (`otelpgx`); `OpenUntraced` is for the status prober so health probes do not create a trace every ten seconds (Pitfall 4).

> Phase 2a replaces `Migrate` with a version that holds a Postgres advisory lock so two API replicas cannot race the migration (WR-11).

`sqlc.yaml`

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: internal/store/migrations
    queries: internal/store/queries
    gen:
      go:
        package: store
        out: internal/store
        sql_package: pgx/v5
```
`internal/store/migrations/00001_users.sql`

```sql
-- +goose Up
create table users (
    github_id     bigint primary key,
    login         text not null,
    avatar_url    text not null default '',
    first_login_at timestamptz not null default now(),
    last_login_at  timestamptz not null default now()
);

-- +goose Down
drop table users;
```
`internal/store/queries/users.sql`

```sql
-- name: UpsertUser :one
insert into users (github_id, login, avatar_url)
values ($1, $2, $3)
on conflict (github_id) do update
    set login = excluded.login,
        avatar_url = excluded.avatar_url,
        last_login_at = now()
returning *;

-- name: GetUserByGithubID :one
select * from users where github_id = $1;
```
`internal/store/generate.go`

```go
package store

//go:generate go tool sqlc generate -f ../../sqlc.yaml
```
`internal/store/store.go`

```go
// Package store owns Postgres access: pool, embedded goose migrations and the
// sqlc-generated queries.
package store

import (
	"context"
	"embed"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open creates a traced pgx pool.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// OpenUntraced creates a small pool without the otelpgx tracer. The status
// prober uses it so health probes do not create a trace every few seconds.
func OpenUntraced(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 1
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Migrate applies the embedded goose migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	return goose.UpContext(ctx, db, "migrations")
}
```
Generate the sqlc output (after section 12 is in place you can use `bazel run //internal/store:gen`):

```bash
go tool sqlc generate -f sqlc.yaml
```

This writes `db.go`, `models.go`, `users.sql.go`. They are shown below only so you can compare your output; **never edit them by hand**.

`internal/store/db.go`

```go
// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.31.1

package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func New(db DBTX) *Queries {
	return &Queries{db: db}
}

type Queries struct {
	db DBTX
}

func (q *Queries) WithTx(tx pgx.Tx) *Queries {
	return &Queries{
		db: tx,
	}
}
```
`internal/store/models.go`

```go
// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.31.1

package store

import (
	"github.com/jackc/pgx/v5/pgtype"
)

type User struct {
	GithubID     int64
	Login        string
	AvatarUrl    string
	FirstLoginAt pgtype.Timestamptz
	LastLoginAt  pgtype.Timestamptz
}
```
`internal/store/users.sql.go`

```go
// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.31.1
// source: users.sql

package store

import (
	"context"
)

const getUserByGithubID = `-- name: GetUserByGithubID :one
select github_id, login, avatar_url, first_login_at, last_login_at from users where github_id = $1
`

func (q *Queries) GetUserByGithubID(ctx context.Context, githubID int64) (User, error) {
	row := q.db.QueryRow(ctx, getUserByGithubID, githubID)
	var i User
	err := row.Scan(
		&i.GithubID,
		&i.Login,
		&i.AvatarUrl,
		&i.FirstLoginAt,
		&i.LastLoginAt,
	)
	return i, err
}

const upsertUser = `-- name: UpsertUser :one
insert into users (github_id, login, avatar_url)
values ($1, $2, $3)
on conflict (github_id) do update
    set login = excluded.login,
        avatar_url = excluded.avatar_url,
        last_login_at = now()
returning github_id, login, avatar_url, first_login_at, last_login_at
`

type UpsertUserParams struct {
	GithubID  int64
	Login     string
	AvatarUrl string
}

func (q *Queries) UpsertUser(ctx context.Context, arg UpsertUserParams) (User, error) {
	row := q.db.QueryRow(ctx, upsertUser, arg.GithubID, arg.Login, arg.AvatarUrl)
	var i User
	err := row.Scan(
		&i.GithubID,
		&i.Login,
		&i.AvatarUrl,
		&i.FirstLoginAt,
		&i.LastLoginAt,
	)
	return i, err
}
```
---

## 7. `internal/queue`

asynq over Redis. The important idea: **trace context travels inside the task headers**. The API injects the W3C `traceparent` when it enqueues; the worker's middleware extracts it and runs the handler as a child span, so one trace spans API and worker (SC2). Producer and consumer connections are deliberately uninstrumented (Pitfall 4): asynq polls Redis constantly and would flood Tempo.

`internal/queue/tasks.go`

```go
// Package queue holds asynq task types and trace-context propagation.
package queue

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Task type names.
const (
	TypePing      = "telemetry:ping"
	TypeHeartbeat = "telemetry:heartbeat"
)

// InjectHeaders serialises the W3C trace context from ctx into a header map
// suitable for asynq.NewTaskWithHeaders.
func InjectHeaders(ctx context.Context) map[string]string {
	h := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(h))
	return h
}
```
`internal/queue/client.go`

```go
package queue

import (
	"context"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Client wraps an asynq producer. It owns its own Redis connection, which is
// deliberately not instrumented (Pitfall 4).
type Client struct {
	c *asynq.Client
}

// NewClient connects an asynq client to redisAddr.
func NewClient(redisAddr string) *Client {
	return &Client{c: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})}
}

// Close releases the Redis connection.
func (c *Client) Close() error { return c.c.Close() }

// EnqueuePing publishes a telemetry:ping task under a producer span and carries
// the W3C trace context in the task headers.
func (c *Client) EnqueuePing(ctx context.Context) error {
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, "telemetry:ping publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", "default"),
		))
	defer span.End()

	task := asynq.NewTaskWithHeaders(TypePing, nil, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, asynq.MaxRetry(1)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
```
`internal/queue/middleware.go`

```go
package queue

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware extracts the W3C trace context from the task headers,
// runs the handler under a consumer span, and records task metrics. Metric
// attributes are limited to task.type and outcome (no task IDs).
func TracingMiddleware(next asynq.Handler) asynq.Handler {
	meter := otel.Meter("ephematool/queue")
	processed, _ := meter.Int64Counter("ephematool.queue.task.processed",
		metric.WithDescription("Tasks processed by the worker"))
	duration, _ := meter.Float64Histogram("ephematool.queue.task.duration",
		metric.WithUnit("s"), metric.WithDescription("Task processing duration"))
	tracer := otel.Tracer("ephematool/queue")

	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(t.Headers()))
		ctx, span := tracer.Start(ctx, t.Type()+" process",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "asynq"),
				attribute.String("messaging.operation.type", "process"),
				attribute.String("messaging.destination.name", "default"),
			))
		defer span.End()

		start := time.Now()
		err := next.ProcessTask(ctx, t)

		outcome := "success"
		if err != nil {
			outcome = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		attrs := metric.WithAttributes(
			attribute.String("task.type", t.Type()),
			attribute.String("outcome", outcome),
		)
		processed.Add(ctx, 1, attrs)
		duration.Record(ctx, time.Since(start).Seconds(), attrs)
		return err
	})
}
```
`internal/queue/server.go`

```go
package queue

import (
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// NewServer builds the asynq consumer. It uses a plain RedisClientOpt, so its
// polling traffic is never traced (Pitfall 4).
func NewServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 4,
		Queues:      map[string]int{"default": 1},
	})
}

// NewMux registers the task handlers wrapped in TracingMiddleware. rdb is the
// instrumented application client used by the heartbeat.
func NewMux(rdb *redis.Client) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(TracingMiddleware)
	mux.HandleFunc(TypePing, pingHandler)
	mux.HandleFunc(TypeHeartbeat, heartbeatHandler(rdb))
	return mux
}

// NewScheduler registers the periodic heartbeat task.
func NewScheduler(redisAddr string, every time.Duration) (*asynq.Scheduler, error) {
	s := asynq.NewScheduler(asynq.RedisClientOpt{Addr: redisAddr}, nil)
	uniq := every - time.Second
	if uniq < time.Second {
		uniq = time.Second
	}
	if _, err := s.Register("@every "+every.String(), asynq.NewTask(TypeHeartbeat, nil),
		asynq.Unique(uniq), asynq.MaxRetry(0)); err != nil {
		return nil, err
	}
	return s, nil
}
```
`internal/queue/heartbeat.go`

```go
package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// Heartbeat key written by the worker; the status endpoint reads it.
const (
	HeartbeatKey = "worker:heartbeat"
	HeartbeatTTL = 45 * time.Second
)

// heartbeatHandler writes the current unix time to HeartbeatKey with a TTL.
func heartbeatHandler(rdb *redis.Client) asynq.HandlerFunc {
	return func(ctx context.Context, _ *asynq.Task) error {
		if err := rdb.Set(ctx, HeartbeatKey, time.Now().Unix(), HeartbeatTTL).Err(); err != nil {
			return err
		}
		slog.InfoContext(ctx, "worker heartbeat")
		return nil
	}
}

func pingHandler(ctx context.Context, _ *asynq.Task) error {
	slog.InfoContext(ctx, "ping handled")
	return nil
}
```
`internal/queue/middleware_test.go`

```go
package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func setupTracing(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return exp
}

func TestPropagation(t *testing.T) {
	exp := setupTracing(t)

	pctx, parent := otel.Tracer("test").Start(context.Background(), "api")
	headers := InjectHeaders(pctx)
	parent.End()
	task := asynq.NewTaskWithHeaders(TypePing, nil, headers)

	var handlerSpan trace.SpanContext
	h := TracingMiddleware(asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		handlerSpan = trace.SpanContextFromContext(ctx)
		return nil
	}))
	require.NoError(t, h.ProcessTask(context.Background(), task))

	assert.Equal(t, parent.SpanContext().TraceID(), handlerSpan.TraceID())

	var found bool
	for _, s := range exp.GetSpans() {
		if s.Name != "telemetry:ping process" {
			continue
		}
		found = true
		assert.Equal(t, parent.SpanContext().SpanID(), s.Parent.SpanID())
		assert.Equal(t, trace.SpanKindConsumer, s.SpanKind)
	}
	assert.True(t, found, "consumer span not exported")
}

func TestNoHeadersStartsRoot(t *testing.T) {
	exp := setupTracing(t)
	task := asynq.NewTask(TypeHeartbeat, nil)
	h := TracingMiddleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }))
	require.NotPanics(t, func() { require.NoError(t, h.ProcessTask(context.Background(), task)) })

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, spans[0].Parent.IsValid())
}

func TestTaskMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	setupTracing(t)

	ok := TracingMiddleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }))
	bad := TracingMiddleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return errors.New("boom") }))
	require.NoError(t, ok.ProcessTask(context.Background(), asynq.NewTask(TypePing, nil)))
	require.Error(t, bad.ProcessTask(context.Background(), asynq.NewTask(TypePing, nil)))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	counts := map[string]int64{}
	var durPoints int
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name != "ephematool.queue.task.processed" {
					continue
				}
				for _, dp := range d.DataPoints {
					assert.Equal(t, 2, dp.Attributes.Len(), "only task.type and outcome")
					tt, _ := dp.Attributes.Value("task.type")
					assert.Equal(t, TypePing, tt.AsString())
					o, _ := dp.Attributes.Value("outcome")
					counts[o.AsString()] += dp.Value
				}
			case metricdata.Histogram[float64]:
				if m.Name != "ephematool.queue.task.duration" {
					continue
				}
				for _, dp := range d.DataPoints {
					assert.Equal(t, 2, dp.Attributes.Len())
					durPoints += int(dp.Count)
				}
			}
		}
	}
	assert.Equal(t, map[string]int64{"success": 1, "error": 1}, counts)
	assert.Equal(t, 2, durPoints)
}
```
---

## 8. `internal/status`

A background prober checks the ten fixed services every `PROBE_INTERVAL`, caches the result, and `/api/status` serves the cache. Request handling never blocks on a probe. Platform services (api, worker, dashboard) also report a "last signal" time from Prometheus; if telemetry is older than 60 s the row turns *degraded* even though the process is up. That is what makes the page tell the truth about telemetry rather than only liveness.

`internal/status/model.go`

```go
// Package status probes the platform's own services and keeps a cached snapshot
// for /api/status. Probing is deliberately uninstrumented: it must not create
// a trace per probe (RESEARCH Pitfall 4).
package status

import "time"

// Health states reported to the UI.
const (
	Healthy  = "healthy"
	Degraded = "degraded"
	Down     = "down"
	Unknown  = "unknown"
)

// Service groups, matching the API enum.
const (
	GroupPlatform = "platform"
	GroupData     = "data"
	GroupPipeline = "pipeline"
	GroupBackends = "backends"
)

// Service is one fixed row of the status table.
type Service struct {
	ID, Name, Group string
}

// Services is the contract order of the status page.
var Services = []Service{
	{"api", "API", GroupPlatform},
	{"worker", "Worker", GroupPlatform},
	{"dashboard", "Dashboard", GroupPlatform},
	{"postgres", "Postgres", GroupData},
	{"redis", "Redis", GroupData},
	{"otel-collector", "OTel Collector", GroupPipeline},
	{"tempo", "Tempo", GroupBackends},
	{"loki", "Loki", GroupBackends},
	{"prometheus", "Prometheus", GroupBackends},
	{"grafana", "Grafana", GroupBackends},
}

// Result is the cached health of one service.
type Result struct {
	ID           string
	Status       string
	LastSignalAt *time.Time
	Detail       string
}

// Prometheus service_name values of the services that emit their own telemetry.
var signalServiceNames = map[string]string{
	"api":       "ephematool-api",
	"worker":    "ephematool-worker",
	"dashboard": "ephematool-dashboard",
}

const (
	// StaleSignal is how old a platform service's last telemetry may be before
	// the row turns degraded.
	StaleSignal = 60 * time.Second
	// HeartbeatMaxAge matches queue.HeartbeatTTL.
	HeartbeatMaxAge = 45 * time.Second
)
```
`internal/status/prober.go`

```go
package status

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Checks are the probe functions. Every field is optional; a missing check
// leaves its row "unknown".
type Checks struct {
	Postgres    func(ctx context.Context) error
	Redis       func(ctx context.Context) error
	HeartbeatAt func(ctx context.Context) (time.Time, error)
	// HTTP maps a service id to a readiness URL taken from server config only.
	HTTP map[string]string
	// LastSignal returns the newest telemetry timestamp per Prometheus service_name.
	LastSignal func(ctx context.Context) (map[string]time.Time, error)
	Now        func() time.Time
}

// Prober runs the checks on an interval and caches the results.
type Prober struct {
	c        Checks
	interval time.Duration
	timeout  time.Duration
	client   *http.Client // plain client with no tracing transport, so probes create no spans

	up       metric.Int64Gauge
	duration metric.Float64Histogram

	mu          sync.RWMutex
	generatedAt time.Time
	results     []Result
}

// NewProber builds a prober. meter may be a no-op meter.
func NewProber(c Checks, interval, timeout time.Duration, meter metric.Meter) *Prober {
	if c.Now == nil {
		c.Now = time.Now
	}
	p := &Prober{
		c:        c,
		interval: interval,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
	}
	p.up, _ = meter.Int64Gauge("ephematool.service.up",
		metric.WithDescription("1 when the last probe of the service succeeded"))
	p.duration, _ = meter.Float64Histogram("ephematool.probe.duration",
		metric.WithUnit("s"), metric.WithDescription("Probe duration per service"))
	p.results = make([]Result, len(Services))
	for i, s := range Services {
		p.results[i] = Result{ID: s.ID, Status: Unknown}
	}
	return p
}

// Snapshot returns the cached results in Services order. It never blocks on a probe.
func (p *Prober) Snapshot() (generatedAt time.Time, results []Result) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Result, len(p.results))
	copy(out, p.results)
	return p.generatedAt, out
}

// Run probes immediately and then every interval until ctx ends.
func (p *Prober) Run(ctx context.Context) {
	p.probeAll(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.probeAll(ctx)
		}
	}
}

type probeOut struct {
	err      error
	duration time.Duration
}

// probeAll runs every probe concurrently, then publishes one snapshot.
func (p *Prober) probeAll(ctx context.Context) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		probed  = map[string]probeOut{}
		signals map[string]time.Time
		sigErr  error
		hbAt    time.Time
		hbErr   error
	)
	run := func(id string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, p.timeout)
			defer cancel()
			start := time.Now()
			err := fn(cctx)
			mu.Lock()
			probed[id] = probeOut{err: err, duration: time.Since(start)}
			mu.Unlock()
		}()
	}

	for id, url := range p.c.HTTP {
		run(id, func(c context.Context) error { return p.httpProbe(c, url) })
	}
	if p.c.Postgres != nil {
		run("postgres", p.c.Postgres)
	}
	if p.c.Redis != nil {
		run("redis", p.c.Redis)
	}
	if p.c.HeartbeatAt != nil {
		run("worker", func(c context.Context) error {
			hbAt, hbErr = p.c.HeartbeatAt(c)
			return hbErr
		})
	}
	if p.c.LastSignal != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, p.timeout)
			defer cancel()
			signals, sigErr = p.c.LastSignal(cctx)
		}()
	}
	wg.Wait()

	now := p.c.Now()
	results := make([]Result, len(Services))
	for i, s := range Services {
		results[i] = p.evaluate(s, now, probed, hbAt, hbErr, signals, sigErr)
	}

	p.mu.Lock()
	prev := p.results
	p.results = results
	p.generatedAt = now.UTC()
	p.mu.Unlock()

	for i, r := range results {
		id := r.ID
		up := int64(0)
		if r.Status == Healthy || r.Status == Degraded {
			up = 1
		}
		attrs := metric.WithAttributes(attribute.String("service", id))
		p.up.Record(ctx, up, attrs)
		if o, ok := probed[id]; ok {
			p.duration.Record(ctx, o.duration.Seconds(), attrs)
		}
		logTransition(ctx, id, prev[i].Status, r.Status)
	}
}

func logTransition(ctx context.Context, id, from, to string) {
	if from == to || from == Unknown && to == Healthy {
		return
	}
	switch to {
	case Down, Degraded:
		slog.WarnContext(ctx, "status.transition", "service", id, "from", from, "to", to)
	default:
		slog.InfoContext(ctx, "status.transition", "service", id, "from", from, "to", to)
	}
}

func (p *Prober) httpProbe(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return httpStatusError(resp.StatusCode)
	}
	return nil
}

type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

// detailFor maps a probe error to a short, client-safe string (T-01-34).
func detailFor(err error) string {
	var hs httpStatusError
	var ne net.Error
	switch {
	case errors.As(err, &hs):
		return hs.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	default:
		return "unreachable"
	}
}

func (p *Prober) evaluate(s Service, now time.Time, probed map[string]probeOut,
	hbAt time.Time, hbErr error, signals map[string]time.Time, sigErr error) Result {
	r := Result{ID: s.ID}

	// Probe outcome: the api row is healthy because this code is running.
	var probeErr error
	probeRan := s.ID == "api"
	if o, ok := probed[s.ID]; ok {
		probeRan = true
		probeErr = o.err
	}
	if !probeRan {
		r.Status = Unknown
		return r
	}
	if s.ID == "worker" && probeErr == nil && now.Sub(hbAt) > HeartbeatMaxAge {
		probeErr = errors.New("stale heartbeat")
		r.Detail = "stale heartbeat"
	}
	if probeErr != nil {
		r.Status = Down
		if r.Detail == "" {
			r.Detail = detailFor(probeErr)
		}
		if s.ID == "worker" && hbErr != nil {
			r.Detail = "no heartbeat"
		}
		return r
	}

	if s.Group != GroupPlatform {
		t := now
		r.LastSignalAt = &t
		r.Status = Healthy
		return r
	}

	// Platform rows also need recent telemetry from Prometheus.
	if p.c.LastSignal == nil || sigErr != nil {
		r.Status = Unknown
		r.Detail = "telemetry query unavailable"
		return r
	}
	ts, ok := signals[signalServiceNames[s.ID]]
	if !ok {
		r.Status = Degraded
		r.Detail = "no telemetry signal"
		return r
	}
	r.LastSignalAt = &ts
	if now.Sub(ts) > StaleSignal {
		r.Status = Degraded
		r.Detail = "stale telemetry signal"
		return r
	}
	r.Status = Healthy
	return r
}
```
`internal/status/lastsignal.go`

```go
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ephematool/ephematool/internal/telemetry"
)

// NewLastSignal returns a LastSignal check that queries Prometheus with the
// constant PromQL from internal/telemetry. baseURL comes from server config only.
func NewLastSignal(baseURL string, timeout time.Duration) func(context.Context) (map[string]time.Time, error) {
	// Plain client on purpose (no tracing transport), so queries create no traces.
	client := &http.Client{Timeout: timeout}
	base := strings.TrimRight(baseURL, "/")
	return func(ctx context.Context) (map[string]time.Time, error) {
		out := map[string]time.Time{}
		for _, q := range []string{telemetry.LastSignalQuery, telemetry.DashboardLastSignalQuery} {
			if err := queryVector(ctx, client, base, q, out); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func queryVector(ctx context.Context, client *http.Client, base, query string, out map[string]time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v1/query?query="+url.QueryEscape(query), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus query: HTTP %d", resp.StatusCode)
	}
	var body promResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if body.Status != "success" {
		return fmt.Errorf("prometheus query status %q", body.Status)
	}
	for _, r := range body.Data.Result {
		name := r.Metric[telemetry.LabelServiceName]
		s, ok := r.Value[1].(string)
		if name == "" || !ok {
			continue
		}
		secs, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}
		ts := time.Unix(0, int64(secs*float64(time.Second))).UTC()
		if cur, seen := out[name]; !seen || ts.After(cur) {
			out[name] = ts
		}
	}
	return nil
}
```
`internal/status/prober_test.go`

```go
package status

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var fixedNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// fixture serves 200 for every HTTP target unless overridden.
type fixture struct {
	srv    *httptest.Server
	codes  map[string]int
	sleeps map[string]time.Duration
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{codes: map[string]int{}, sleeps: map[string]time.Duration{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d := f.sleeps[r.URL.Path]; d > 0 {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		if c := f.codes[r.URL.Path]; c != 0 {
			w.WriteHeader(c)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) checks() Checks {
	fresh := fixedNow.Add(-5 * time.Second)
	return Checks{
		Postgres:    func(context.Context) error { return nil },
		Redis:       func(context.Context) error { return nil },
		HeartbeatAt: func(context.Context) (time.Time, error) { return fixedNow.Add(-3 * time.Second), nil },
		HTTP: map[string]string{
			"dashboard":      f.srv.URL + "/dashboard",
			"otel-collector": f.srv.URL + "/collector",
			"tempo":          f.srv.URL + "/tempo",
			"loki":           f.srv.URL + "/loki",
			"prometheus":     f.srv.URL + "/prometheus",
			"grafana":        f.srv.URL + "/grafana",
		},
		LastSignal: func(context.Context) (map[string]time.Time, error) {
			return map[string]time.Time{
				"ephematool-api":       fresh,
				"ephematool-worker":    fresh,
				"ephematool-dashboard": fresh,
			}, nil
		},
		Now: func() time.Time { return fixedNow },
	}
}

func byID(rs []Result) map[string]Result {
	m := make(map[string]Result, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

func probeOnce(p *Prober) []Result {
	p.probeAll(context.Background())
	_, rs := p.Snapshot()
	return rs
}

func TestSnapshotOrder(t *testing.T) {
	f := newFixture(t)
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	_, rs := p.Snapshot()
	require.Len(t, rs, 10)
	want := []string{"api", "worker", "dashboard", "postgres", "redis", "otel-collector", "tempo", "loki", "prometheus", "grafana"}
	for i, r := range rs {
		assert.Equal(t, want[i], r.ID)
		assert.Equal(t, "unknown", r.Status)
		assert.Equal(t, want[i], Services[i].ID)
	}
}

func TestHealthy(t *testing.T) {
	f := newFixture(t)
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	rs := probeOnce(p)
	require.Len(t, rs, 10)
	for _, r := range rs {
		assert.Equal(t, "healthy", r.Status, r.ID)
		require.NotNil(t, r.LastSignalAt, r.ID)
	}
	m := byID(rs)
	assert.Equal(t, fixedNow, *m["loki"].LastSignalAt)
	assert.Equal(t, fixedNow, *m["postgres"].LastSignalAt)
	assert.Equal(t, fixedNow.Add(-5*time.Second), *m["api"].LastSignalAt)
}

func TestDownOnProbeFailure(t *testing.T) {
	f := newFixture(t)
	f.codes["/loki"] = http.StatusServiceUnavailable
	c := f.checks()
	c.Postgres = func(context.Context) error { return errors.New("boom: secret-host:5432") }
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	m := byID(probeOnce(p))
	assert.Equal(t, "down", m["loki"].Status)
	assert.Contains(t, m["loki"].Detail, "503")
	assert.Equal(t, "down", m["postgres"].Status)
	// T-01-34: no raw error text leaks to the client.
	assert.NotContains(t, m["postgres"].Detail, "secret-host")
	assert.Equal(t, "healthy", m["tempo"].Status)
}

func TestDegradedStaleSignal(t *testing.T) {
	f := newFixture(t)
	c := f.checks()
	c.LastSignal = func(context.Context) (map[string]time.Time, error) {
		return map[string]time.Time{
			"ephematool-api":       fixedNow.Add(-2 * time.Second),
			"ephematool-worker":    fixedNow.Add(-2 * time.Second),
			"ephematool-dashboard": fixedNow.Add(-61 * time.Second),
		}, nil
	}
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	m := byID(probeOnce(p))
	assert.Equal(t, "degraded", m["dashboard"].Status)
	assert.Equal(t, "healthy", m["api"].Status)
	assert.Equal(t, "healthy", m["worker"].Status)
}

func TestUnknownWhenPrometheusDown(t *testing.T) {
	f := newFixture(t)
	c := f.checks()
	c.LastSignal = func(context.Context) (map[string]time.Time, error) { return nil, errors.New("prometheus unreachable") }
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	m := byID(probeOnce(p))
	for _, id := range []string{"api", "worker", "dashboard"} {
		assert.Equal(t, "unknown", m[id].Status, id)
	}
	// The prometheus row follows only its own ready probe.
	assert.Equal(t, "healthy", m["prometheus"].Status)

	f.codes["/prometheus"] = http.StatusServiceUnavailable
	m = byID(probeOnce(p))
	assert.Equal(t, "down", m["prometheus"].Status)
}

func TestWorkerHeartbeat(t *testing.T) {
	f := newFixture(t)
	c := f.checks()
	c.HeartbeatAt = func(context.Context) (time.Time, error) { return time.Time{}, errors.New("redis: nil") }
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	assert.Equal(t, "down", byID(probeOnce(p))["worker"].Status)

	c.HeartbeatAt = func(context.Context) (time.Time, error) { return fixedNow.Add(-46 * time.Second), nil }
	p = NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	assert.Equal(t, "down", byID(probeOnce(p))["worker"].Status)
}

func TestProbeTimeout(t *testing.T) {
	f := newFixture(t)
	f.sleeps["/tempo"] = 3 * time.Second
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))

	done := make(chan struct{})
	start := time.Now()
	go func() {
		p.probeAll(context.Background())
		close(done)
	}()

	// Snapshot must not block while the probe is in flight.
	time.Sleep(100 * time.Millisecond)
	snapStart := time.Now()
	_, rs := p.Snapshot()
	assert.Less(t, time.Since(snapStart), 200*time.Millisecond)
	assert.Len(t, rs, 10)

	<-done
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 2800*time.Millisecond)
	assert.GreaterOrEqual(t, elapsed, 1900*time.Millisecond)
	m := byID(func() []Result { _, r := p.Snapshot(); return r }())
	assert.Equal(t, "down", m["tempo"].Status)
	assert.Equal(t, "timeout", m["tempo"].Detail)
}

func TestProbeMetrics(t *testing.T) {
	f := newFixture(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, mp.Meter("test"))
	probeOnce(p)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	seenUp := map[string]bool{}
	seenDur := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "ephematool.service.up":
				g, ok := m.Data.(metricdata.Gauge[int64])
				require.True(t, ok)
				for _, dp := range g.DataPoints {
					require.Equal(t, 1, dp.Attributes.Len(), "only the service attribute")
					v, _ := dp.Attributes.Value("service")
					seenUp[v.AsString()] = true
					assert.Equal(t, int64(1), dp.Value)
				}
			case "ephematool.probe.duration":
				seenDur = true
			}
		}
	}
	assert.Len(t, seenUp, 10)
	assert.True(t, seenDur)
}
```
---

## 9. `internal/fakegithub` (test-only)

A stdlib-only fake of the GitHub OAuth and REST endpoints the login uses: authorize (with PKCE check), token exchange, `/user`, org membership, team membership, with six fixture users that cover every allowlist outcome (admin, viewer, team-only, pending, outsider, 403-forbidden). It lets the entire login flow run in tests and in `bazel run //:up_fake` without a real OAuth App. It must never be in the base stack (T-01-26).

`internal/fakegithub/server.go`

```go
// Package fakegithub is a stdlib-only fake of the GitHub OAuth and REST
// endpoints the login flow uses. It is test-only: it ships in tests and in
// deploy/compose.e2e.yaml, never in the base stack (T-01-26).
package fakegithub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// ClientID and ClientSecret are the fake OAuth App credentials.
	ClientID     = "e2e-client"
	ClientSecret = "e2e-secret"
	// Org is the fixture organization; Team is the fixture "org/slug" team.
	Org  = "ephematool-e2e"
	Team = "partner-org/sre"
)

// Fixture describes one fake GitHub user.
type Fixture struct {
	Login string
	ID    int64
	// Orgs maps org -> membership state ("active" | "pending").
	Orgs map[string]string
	// Teams maps "org/slug" -> membership state.
	Teams map[string]string
	// OrgForbidden makes the org membership endpoint return 403.
	OrgForbidden bool
}

// Fixtures returns the canonical fixture users.
func Fixtures() []Fixture {
	return []Fixture{
		{Login: "octo-admin", ID: 1001, Orgs: map[string]string{Org: "active"}},
		{Login: "octo-viewer", ID: 1002, Orgs: map[string]string{Org: "active"}},
		{Login: "octo-teamonly", ID: 1003, Teams: map[string]string{Team: "active"}},
		{Login: "octo-pending", ID: 1004, Orgs: map[string]string{Org: "pending"}},
		{Login: "octo-outsider", ID: 1005},
		{Login: "octo-forbidden", ID: 1006, OrgForbidden: true},
	}
}

type grant struct {
	challenge string
	login     string
}

// Server implements http.Handler.
type Server struct {
	mux      *http.ServeMux
	fixtures map[string]Fixture

	mu            sync.Mutex
	codes         map[string]grant
	lastAuthorize url.Values
}

// New returns a fake GitHub with the canonical fixtures.
func New() *Server {
	s := &Server{
		mux:      http.NewServeMux(),
		fixtures: map[string]Fixture{},
		codes:    map[string]grant{},
	}
	for _, f := range Fixtures() {
		s.fixtures[f.Login] = f
	}
	s.mux.HandleFunc("GET /login/oauth/authorize", s.authorize)
	s.mux.HandleFunc("POST /login/oauth/access_token", s.accessToken)
	s.mux.HandleFunc("GET /user", s.user)
	s.mux.HandleFunc("GET /user/memberships/orgs/{org}", s.orgMembership)
	s.mux.HandleFunc("GET /orgs/{org}/teams/{slug}/memberships/{login}", s.teamMembership)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// LastAuthorize returns the query of the most recent authorize request.
func (s *Server) LastAuthorize() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := url.Values{}
	for k, v := range s.lastAuthorize {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.lastAuthorize = q
	s.mu.Unlock()

	redirect := q.Get("redirect_uri")
	ru, err := url.Parse(redirect)
	if redirect == "" || err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	back := func(extra url.Values) {
		rq := ru.Query()
		rq.Set("state", q.Get("state"))
		for k, v := range extra {
			rq.Set(k, v[0])
		}
		ru.RawQuery = rq.Encode()
		http.Redirect(w, r, ru.String(), http.StatusFound)
	}
	if q.Get("cancel") != "" {
		back(url.Values{"error": {"access_denied"}})
		return
	}
	if login := q.Get("login"); login != "" {
		if _, ok := s.fixtures[login]; !ok {
			http.Error(w, "unknown fixture", http.StatusBadRequest)
			return
		}
		code := randToken()
		s.mu.Lock()
		s.codes[code] = grant{challenge: q.Get("code_challenge"), login: login}
		s.mu.Unlock()
		back(url.Values{"code": {code}})
		return
	}

	logins := make([]string, 0, len(s.fixtures))
	for l := range s.fixtures {
		logins = append(logins, l)
	}
	sort.Strings(logins)
	var b strings.Builder
	b.WriteString("<!doctype html><title>Fake GitHub</title><h1>Authorize (fake GitHub)</h1>")
	b.WriteString(`<form method="get" action="/login/oauth/authorize">`)
	for k, vs := range q {
		if k == "login" || k == "cancel" {
			continue
		}
		b.WriteString(`<input type="hidden" name="` + html.EscapeString(k) + `" value="` + html.EscapeString(vs[0]) + `">`)
	}
	for _, l := range logins {
		b.WriteString(`<p><button type="submit" name="login" value="` + html.EscapeString(l) + `">` + html.EscapeString(l) + `</button></p>`)
	}
	b.WriteString(`<p><button type="submit" name="cancel" value="1">Cancel</button></p></form>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) accessToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	id, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	if u, p, ok := r.BasicAuth(); ok {
		id, secret = u, p
	}
	if id != ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(ClientSecret)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "incorrect_client_credentials"})
		return
	}
	code := r.PostForm.Get("code")
	s.mu.Lock()
	g, ok := s.codes[code]
	delete(s.codes, code) // single use
	s.mu.Unlock()
	verifier := r.PostForm.Get("code_verifier")
	if !ok || g.challenge == "" || verifier == "" ||
		subtle.ConstantTimeCompare([]byte(s256(verifier)), []byte(g.challenge)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_verification_code"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": "fake-token-" + g.login,
		"token_type":   "bearer",
		"scope":        "read:org",
	})
}

// caller resolves the bearer token to a fixture.
func (s *Server) caller(w http.ResponseWriter, r *http.Request) (Fixture, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	tok = strings.TrimPrefix(tok, "token ")
	f, ok := s.fixtures[strings.TrimPrefix(tok, "fake-token-")]
	if !ok || !strings.HasPrefix(tok, "fake-token-") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return Fixture{}, false
	}
	return f, true
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	f, ok := s.caller(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"login":      f.Login,
		"id":         f.ID,
		"avatar_url": "https://avatars.example.invalid/u/" + strconv.FormatInt(f.ID, 10),
	})
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func (s *Server) orgMembership(w http.ResponseWriter, r *http.Request) {
	f, ok := s.caller(w, r)
	if !ok {
		return
	}
	if f.OrgForbidden {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible"})
		return
	}
	org := r.PathValue("org")
	state, ok := f.Orgs[org]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":        state,
		"role":         "member",
		"organization": map[string]string{"login": org},
		"user":         map[string]string{"login": f.Login},
	})
}

func (s *Server) teamMembership(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.caller(w, r); !ok {
		return
	}
	f, ok := s.fixtures[r.PathValue("login")]
	if !ok {
		notFound(w)
		return
	}
	state, ok := f.Teams[r.PathValue("org")+"/"+r.PathValue("slug")]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": state, "role": "member"})
}
```
`internal/fakegithub/server_test.go`

```go
package fakegithub

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func nonFollowing() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// authorizeCode drives authorize for a login and returns the issued code.
func authorizeCode(t *testing.T, base, login, challenge string) string {
	t.Helper()
	q := url.Values{
		"client_id": {ClientID}, "redirect_uri": {"http://app.test/cb"}, "state": {"s"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "login": {login},
	}
	resp, err := nonFollowing().Get(base + "/login/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if loc.Query().Get("state") != "s" {
		t.Fatalf("state not echoed: %v", loc)
	}
	return loc.Query().Get("code")
}

func exchange(t *testing.T, base, code, verifier string) *http.Response {
	t.Helper()
	form := url.Values{
		"client_id": {ClientID}, "client_secret": {ClientSecret}, "code": {code}, "code_verifier": {verifier},
		"grant_type": {"authorization_code"},
	}
	resp, err := http.Post(base+"/login/oauth/access_token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPKCE(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	verifier := "verifier-verifier-verifier-verifier-verifier-1234"

	code := authorizeCode(t, ts.URL, "octo-admin", s256(verifier))
	bad := exchange(t, ts.URL, code, "wrong-verifier")
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong verifier: got %d, want 400", bad.StatusCode)
	}

	code = authorizeCode(t, ts.URL, "octo-admin", s256(verifier))
	ok := exchange(t, ts.URL, code, verifier)
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("correct verifier: got %d, want 200", ok.StatusCode)
	}
}

func membership(t *testing.T, base, login, path string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer fake-token-"+login)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	b := make([]byte, 512)
	n, _ := resp.Body.Read(b)
	buf.Write(b[:n])
	return resp.StatusCode, buf.String()
}

func TestMemberships(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	org := "/user/memberships/orgs/ephematool-e2e"
	cases := []struct {
		login, path string
		status      int
		state       string
	}{
		{"octo-admin", org, 200, `"active"`},
		{"octo-pending", org, 200, `"pending"`},
		{"octo-outsider", org, 404, ""},
		{"octo-forbidden", org, 403, ""},
		{"octo-teamonly", "/orgs/partner-org/teams/sre/memberships/octo-teamonly", 200, `"active"`},
	}
	for _, c := range cases {
		status, body := membership(t, ts.URL, c.login, c.path)
		if status != c.status || (c.state != "" && !strings.Contains(body, c.state)) {
			t.Errorf("%s %s: got %d %s", c.login, c.path, status, body)
		}
	}
}
```
`cmd/fakegithub/main.go`

```go
// Command fakegithub runs the fake GitHub for dev/e2e login. Test-only.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ephematool/ephematool/internal/fakegithub"
)

func main() {
	addr := os.Getenv("FAKEGITHUB_ADDR")
	if addr == "" {
		addr = ":9999"
	}
	fmt.Println("fakegithub listening on", addr)
	srv := &http.Server{Addr: addr, Handler: fakegithub.New(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
```
---

## 10. `internal/auth`

The login flow, step by step (each step is visible in `handlers.go`):

1. `GET /api/auth/github/login` generates `state` and a PKCE verifier, stores them with a sanitised `next` path in a short-lived `HttpOnly` cookie scoped to `/api/auth/github`, and redirects to GitHub.
2. `GET /api/auth/github/callback` checks the cookie and `state` in constant time, exchanges the code (with the verifier), reads the user, and evaluates the allowlist **fail-closed**: only an `active` membership in a configured org or `org/team` allows sign-in. Pending, 404, 403, transport errors and an empty allowlist all deny.
3. On success it upserts the user, renews the session token (session-fixation defence), stores `user_id/login/avatar/role/orgs`, and redirects to `next`.
4. The GitHub access token is a local variable only. It is never stored, logged, or put in a span.

Role is `admin` when the login is in `ADMIN_LOGINS` (case-insensitive), `viewer` otherwise. Phase 2 adds `creator` and a request-time resolver.

`internal/auth/session.go`

```go
package auth

import (
	"net/http"

	"github.com/alexedwards/scs/goredisstore"
	"github.com/alexedwards/scs/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/config"
)

// CookieName is the session cookie name.
const CookieName = "ephematool_session"

// Session keys.
const (
	KeyUserID    = "user_id"
	KeyLogin     = "login"
	KeyAvatarURL = "avatar_url"
	KeyRole      = "role"
	KeyOrgs      = "orgs"
)

// NewSessionManager builds the Redis-backed session manager. IdleTimeout (D-03)
// and Lifetime (D-14) are set explicitly because scs defaults to a 24h
// absolute lifetime (Pitfall 1). scs re-commits (slides) a session on every request while IdleTimeout > 0.
func NewSessionManager(rdb *redis.Client, cfg config.Config) *scs.SessionManager {
	sm := scs.New()
	sm.Store = goredisstore.New(rdb)
	sm.IdleTimeout = cfg.SessionIdleTimeout
	sm.Lifetime = cfg.SessionLifetime
	sm.Cookie.Name = CookieName
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = cfg.CookieSecure // Pitfall 11: true behind TLS
	sm.Cookie.Persist = true
	sm.Cookie.Path = "/"
	return sm
}
```
`internal/auth/next.go`

```go
package auth

import (
	"net/url"
	"strings"
)

// SafeNext returns raw only when it is a single-slash relative path, else "/".
// It rejects protocol-relative ("//host"), "/\" tricks, schemes, hosts and
// control characters (open redirect, T-01-19).
func SafeNext(raw string) string {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.ContainsRune(raw, '\\') {
		return "/"
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "/"
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return raw
}
```
`internal/auth/role.go`

```go
package auth

import "strings"

// RoleFor returns "admin" when login is in adminLogins (case-insensitive, D-02)
// and "viewer" otherwise. The role is evaluated at login and stored in the
// session, so a change to ADMIN_LOGINS applies at the next sign-in (assumption
// A3). Admin pinning by numeric GitHub id is a Phase 2 hardening item.
func RoleFor(login string, adminLogins []string) string {
	for _, a := range adminLogins {
		if strings.EqualFold(strings.TrimSpace(a), login) {
			return "admin"
		}
	}
	return "viewer"
}
```
`internal/auth/allowlist.go`

```go
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-github/v92/github"
)

// Decision is the outcome of an allowlist evaluation (D-01, D-04).
type Decision struct {
	Allowed bool
	// Reason is "org_membership" when denied.
	Reason string
	// GrantedBy lists the orgs ("org") and teams ("org/slug") that allowed the user.
	GrantedBy []string
	// Forbidden is true when an org check returned 403 (OAuth app access restrictions).
	Forbidden bool
}

// MembershipClient abstracts the GitHub membership lookups (faked in tests).
type MembershipClient interface {
	OrgMembership(ctx context.Context, org string) (state string, status int, err error)
	TeamMembership(ctx context.Context, org, slug, login string) (state string, status int, err error)
}

// Allowlist is the configured set of orgs and "org/slug" teams.
type Allowlist struct {
	Orgs  []string
	Teams []string
}

const stateActive = "active"

// Evaluate fails closed: only an "active" membership in at least one
// configured org or team allows the login. Pending, 404, 403, transport
// errors and an empty allowlist all deny.
func (a Allowlist) Evaluate(ctx context.Context, mc MembershipClient, login string) Decision {
	d := Decision{Reason: "org_membership"}
	for _, org := range a.Orgs {
		state, status, err := mc.OrgMembership(ctx, org)
		if status == http.StatusForbidden {
			d.Forbidden = true
			slog.WarnContext(ctx, "auth.org_check_forbidden", "org", org, "login", login)
			continue
		}
		if err == nil && status == http.StatusOK && state == stateActive {
			d.GrantedBy = append(d.GrantedBy, org)
		}
	}
	for _, team := range a.Teams {
		org, slug, ok := strings.Cut(team, "/")
		if !ok || org == "" || slug == "" {
			continue
		}
		state, status, err := mc.TeamMembership(ctx, org, slug, login)
		if err == nil && status == http.StatusOK && state == stateActive {
			d.GrantedBy = append(d.GrantedBy, team)
		}
	}
	if len(d.GrantedBy) > 0 {
		d.Allowed = true
		d.Reason = ""
		d.Forbidden = false
	}
	return d
}

// GitHubMembershipClient implements MembershipClient with go-github, acting as
// the signed-in user (the access token lives only inside the client).
type GitHubMembershipClient struct {
	Client *github.Client
}

func statusOf(resp *github.Response, err error) int {
	if resp != nil && resp.Response != nil {
		return resp.StatusCode
	}
	var er *github.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		return er.Response.StatusCode
	}
	return 0
}

func (g GitHubMembershipClient) OrgMembership(ctx context.Context, org string) (string, int, error) {
	m, resp, err := g.Client.Organizations.GetOrgMembership(ctx, "", org)
	status := statusOf(resp, err)
	if err != nil || m == nil {
		return "", status, err
	}
	return m.GetState(), status, nil
}

func (g GitHubMembershipClient) TeamMembership(ctx context.Context, org, slug, login string) (string, int, error) {
	m, resp, err := g.Client.Teams.GetTeamMembershipBySlug(ctx, org, slug, login)
	status := statusOf(resp, err)
	if err != nil || m == nil {
		return "", status, err
	}
	return m.GetState(), status, nil
}
```
`internal/auth/oauth.go`

```go
package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"

	"github.com/ephematool/ephematool/internal/config"
)

// newOAuthConfig builds the GitHub OAuth App config. read:org is required to
// check org membership (D-15).
func newOAuthConfig(cfg config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.GitHubClientID,
		ClientSecret: cfg.GitHubClientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: cfg.GitHubAuthorizeURL, TokenURL: cfg.GitHubTokenURL},
		RedirectURL:  strings.TrimRight(cfg.PublicBaseURL, "/") + "/api/auth/github/callback",
		Scopes:       []string{"read:org"},
	}
}

// newHTTPClient is the traced client used for the token exchange and GitHub API.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   15 * time.Second,
	}
}

// newGitHubClient returns a go-github client acting as the signed-in user. The
// token is held only by the returned client's transport and is never stored.
func newGitHubClient(hc *http.Client, apiURL, tok string) (*github.Client, error) {
	if !strings.HasSuffix(apiURL, "/") {
		apiURL += "/"
	}
	return github.NewClient(github.WithHTTPClient(hc), github.WithAuthToken(tok), github.WithURLs(&apiURL, nil))
}
```
`internal/auth/middleware.go`

```go
package auth

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
)

// RequireSession returns a Huma middleware that answers 401 for operations
// whose Security includes the "session" scheme when no user is signed in.
func RequireSession(api huma.API, sm *scs.SessionManager) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op == nil || !needsSession(op) {
			next(ctx)
			return
		}
		if sm == nil || sm.GetInt64(ctx.Context(), KeyUserID) == 0 {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		next(ctx)
	}
}

func needsSession(op *huma.Operation) bool {
	for _, req := range op.Security {
		if _, ok := req["session"]; ok {
			return true
		}
	}
	return false
}
```
`internal/auth/handlers.go`

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/oauth2"

	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/store"
)

const oauthCookie = "ephematool_oauth"

// RouteWrapper tags a plain handler with its route template. httpapi supplies
// httpapi.RouteHandler; passing it in avoids an httpapi <-> auth import cycle.
type RouteWrapper func(method, pattern string, h http.Handler) http.Handler

// Handlers serves the login flow and the session-backed API operations.
type Handlers struct {
	cfg    config.Config
	sm     *scs.SessionManager
	q      *store.Queries
	oauth  *oauth2.Config
	hc     *http.Client
	allow  Allowlist
	logins metric.Int64Counter
}

// NewHandlers builds the handlers. sm and q may be nil when only the OpenAPI
// spec is generated.
func NewHandlers(cfg config.Config, sm *scs.SessionManager, q *store.Queries) *Handlers {
	logins, _ := otel.Meter("ephematool/auth").Int64Counter("ephematool.auth.logins",
		metric.WithDescription("GitHub login attempts by outcome"))
	return &Handlers{
		cfg: cfg, sm: sm, q: q,
		oauth:  newOAuthConfig(cfg),
		hc:     newHTTPClient(),
		allow:  Allowlist{Orgs: cfg.AllowedOrgs, Teams: cfg.AllowedTeams},
		logins: logins,
	}
}

// Sessions exposes the session manager (used by httpapi for LoadAndSave).
func (h *Handlers) Sessions() *scs.SessionManager { return h.sm }

func (h *Handlers) count(ctx context.Context, outcome string) {
	if h.logins != nil {
		h.logins.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}

func (h *Handlers) fail(w http.ResponseWriter, r *http.Request, reason string) {
	slog.WarnContext(r.Context(), "auth.failed", "reason", reason)
	h.count(r.Context(), "failed")
	http.Redirect(w, r, "/login?reason=failed", http.StatusFound)
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (h *Handlers) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: oauthCookie, Value: value, Path: "/api/auth/github", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.cfg.CookieSecure,
	}
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.GitHubConfigured() {
		var missing []string
		if h.cfg.GitHubClientID == "" {
			missing = append(missing, "GITHUB_CLIENT_ID")
		}
		if h.cfg.GitHubClientSecret == "" {
			missing = append(missing, "GITHUB_CLIENT_SECRET")
		}
		slog.WarnContext(r.Context(), "auth.github_not_configured", "missing", strings.Join(missing, ","))
		h.count(r.Context(), "failed")
		http.Redirect(w, r, "/login?reason=failed", http.StatusFound)
		return
	}
	state := randomString(32)
	verifier := oauth2.GenerateVerifier()
	next := base64.RawURLEncoding.EncodeToString([]byte(SafeNext(r.URL.Query().Get("next"))))
	http.SetCookie(w, h.cookie(state+"|"+verifier+"|"+next, 600))
	http.Redirect(w, r, h.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (h *Handlers) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, cerr := r.Cookie(oauthCookie)
	http.SetCookie(w, h.cookie("", -1))
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		h.fail(w, r, "provider_"+e)
		return
	}
	if cerr != nil {
		h.fail(w, r, "missing_oauth_cookie")
		return
	}
	parts := strings.SplitN(c.Value, "|", 3)
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 || parts[0] == "" {
		h.fail(w, r, "state_mismatch")
		return
	}
	verifier := parts[1]
	nb, _ := base64.RawURLEncoding.DecodeString(parts[2])
	next := SafeNext(string(nb))
	code := q.Get("code")
	if code == "" {
		h.fail(w, r, "missing_code")
		return
	}

	// The access token is a local variable only: never stored, logged or traced.
	xctx := context.WithValue(ctx, oauth2.HTTPClient, h.hc)
	tok, err := h.oauth.Exchange(xctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		h.fail(w, r, "token_exchange")
		return
	}
	gh, err := newGitHubClient(h.hc, h.cfg.GitHubAPIURL, tok.AccessToken)
	if err != nil {
		h.fail(w, r, "github_client")
		return
	}
	gu, _, err := gh.Users.Get(ctx, "")
	if err != nil || gu.GetLogin() == "" {
		h.fail(w, r, "github_user")
		return
	}
	login, id := gu.GetLogin(), gu.GetID()

	dec := h.allow.Evaluate(ctx, GitHubMembershipClient{Client: gh}, login)
	if !dec.Allowed {
		slog.WarnContext(ctx, "auth.denied", "login", login, "github_id", id, "reason", dec.Reason,
			"required_orgs", strings.Join(h.cfg.AllowedOrgs, ","), "required_teams", strings.Join(h.cfg.AllowedTeams, ","))
		h.count(ctx, "denied")
		http.Redirect(w, r, "/denied?reason=org_membership&login="+url.QueryEscape(login), http.StatusFound)
		return
	}

	if _, err := h.q.UpsertUser(ctx, store.UpsertUserParams{GithubID: id, Login: login, AvatarUrl: gu.GetAvatarURL()}); err != nil {
		h.fail(w, r, "user_upsert")
		return
	}
	role := RoleFor(login, h.cfg.AdminLogins)
	if err := h.sm.RenewToken(ctx); err != nil { // session fixation defense
		h.fail(w, r, "session_renew")
		return
	}
	h.sm.Put(ctx, KeyUserID, id)
	h.sm.Put(ctx, KeyLogin, login)
	h.sm.Put(ctx, KeyAvatarURL, gu.GetAvatarURL())
	h.sm.Put(ctx, KeyRole, role)
	h.sm.Put(ctx, KeyOrgs, dec.GrantedBy)
	slog.InfoContext(ctx, "auth.login", "login", login, "github_id", id, "role", role)
	h.count(ctx, "success")
	http.Redirect(w, r, next, http.StatusFound)
}

// MeBody is the GET /api/me payload.
type MeBody struct {
	Login     string   `json:"login"`
	AvatarURL string   `json:"avatarUrl"`
	Role      string   `json:"role" enum:"admin,viewer"`
	Orgs      []string `json:"orgs"`
}

// MeResponse wraps MeBody for Huma.
type MeResponse struct{ Body MeBody }

// ConfigBody is the GET /api/auth/config payload.
type ConfigBody struct {
	GitHubConfigured bool     `json:"githubConfigured"`
	AllowedOrgs      []string `json:"allowedOrgs"`
	AllowedTeams     []string `json:"allowedTeams"`
}

// ConfigResponse wraps ConfigBody for Huma.
type ConfigResponse struct{ Body ConfigBody }

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Register wires the plain OAuth routes (via wrap) and the Huma operations.
func Register(api huma.API, mux *http.ServeMux, h *Handlers, wrap RouteWrapper) {
	oa := api.OpenAPI()
	if oa.Components == nil {
		oa.Components = &huma.Components{}
	}
	if oa.Components.SecuritySchemes == nil {
		oa.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	oa.Components.SecuritySchemes["session"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: CookieName}
	api.UseMiddleware(RequireSession(api, h.sm))

	mux.Handle("GET /api/auth/github/login", wrap("GET", "/api/auth/github/login", http.HandlerFunc(h.login)))
	mux.Handle("GET /api/auth/github/callback", wrap("GET", "/api/auth/github/callback", http.HandlerFunc(h.callback)))

	huma.Register(api, huma.Operation{
		OperationID: "get-me", Method: http.MethodGet, Path: "/api/me",
		Summary: "Current signed-in user", Security: []map[string][]string{{"session": {}}},
	}, func(ctx context.Context, _ *struct{}) (*MeResponse, error) {
		orgs, _ := h.sm.Get(ctx, KeyOrgs).([]string)
		id := h.sm.GetInt64(ctx, KeyUserID)
		if h.q != nil { // read the persisted user (first real DB read)
			if u, err := h.q.GetUserByGithubID(ctx, id); err == nil {
				return &MeResponse{Body: MeBody{
					Login: u.Login, AvatarURL: u.AvatarUrl, Role: h.sm.GetString(ctx, KeyRole), Orgs: nonNil(orgs),
				}}, nil
			}
		}
		return &MeResponse{Body: MeBody{
			Login: h.sm.GetString(ctx, KeyLogin), AvatarURL: h.sm.GetString(ctx, KeyAvatarURL),
			Role: h.sm.GetString(ctx, KeyRole), Orgs: nonNil(orgs),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-config", Method: http.MethodGet, Path: "/api/auth/config",
		Summary: "Public sign-in configuration",
	}, func(context.Context, *struct{}) (*ConfigResponse, error) {
		return &ConfigResponse{Body: ConfigBody{
			GitHubConfigured: h.cfg.GitHubConfigured(),
			AllowedOrgs:      nonNil(h.cfg.AllowedOrgs),
			AllowedTeams:     nonNil(h.cfg.AllowedTeams),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-logout", Method: http.MethodPost, Path: "/api/auth/logout",
		Summary: "Sign out", DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if h.sm != nil {
			if err := h.sm.Destroy(ctx); err != nil {
				return nil, huma.Error500InternalServerError("could not end session")
			}
		}
		return nil, nil
	})
}
```
### 10.1 Tests

`internal/auth/next_test.go`

```go
package auth

import "testing"

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/":                   "/",
		"/status?x=1":         "/status?x=1",
		"//evil":              "/",
		"https://evil":        "/",
		"/\\evil":             "/",
		"":                    "/",
		"javascript:alert(1)": "/",
		"/a\nb":               "/",
		"status":              "/",
	}
	for in, want := range cases {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
```
`internal/auth/allowlist_test.go`

```go
package auth

import (
	"context"
	"errors"
	"testing"
)

type fakeMC struct {
	orgs  map[string][2]any // org -> {state, status}
	teams map[string][2]any
	err   error
}

func (f fakeMC) OrgMembership(_ context.Context, org string) (string, int, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	v, ok := f.orgs[org]
	if !ok {
		return "", 404, nil
	}
	return v[0].(string), v[1].(int), nil
}

func (f fakeMC) TeamMembership(_ context.Context, org, slug, _ string) (string, int, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	v, ok := f.teams[org+"/"+slug]
	if !ok {
		return "", 404, nil
	}
	return v[0].(string), v[1].(int), nil
}

func TestAllowlist(t *testing.T) {
	al := Allowlist{Orgs: []string{"acme"}, Teams: []string{"partner/sre"}}
	cases := []struct {
		name      string
		al        Allowlist
		mc        fakeMC
		allowed   bool
		forbidden bool
		granted   []string
	}{
		{"org active", al, fakeMC{orgs: map[string][2]any{"acme": {"active", 200}}}, true, false, []string{"acme"}},
		{"org pending", al, fakeMC{orgs: map[string][2]any{"acme": {"pending", 200}}}, false, false, nil},
		{"org 404", al, fakeMC{}, false, false, nil},
		{"org 403", al, fakeMC{orgs: map[string][2]any{"acme": {"", 403}}}, false, true, nil},
		{"transport error", al, fakeMC{err: errors.New("boom")}, false, false, nil},
		{"team active", al, fakeMC{teams: map[string][2]any{"partner/sre": {"active", 200}}}, true, false, []string{"partner/sre"}},
		{"empty allowlist", Allowlist{}, fakeMC{orgs: map[string][2]any{"acme": {"active", 200}}}, false, false, nil},
		{"403 org but team ok", al, fakeMC{orgs: map[string][2]any{"acme": {"", 403}}, teams: map[string][2]any{"partner/sre": {"active", 200}}}, true, false, []string{"partner/sre"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := c.al.Evaluate(context.Background(), c.mc, "u")
			if d.Allowed != c.allowed || d.Forbidden != c.forbidden {
				t.Fatalf("got %+v", d)
			}
			if len(d.GrantedBy) != len(c.granted) || (len(c.granted) > 0 && d.GrantedBy[0] != c.granted[0]) {
				t.Fatalf("granted %v, want %v", d.GrantedBy, c.granted)
			}
			if !d.Allowed && d.Reason != "org_membership" {
				t.Fatalf("reason %q", d.Reason)
			}
		})
	}
}

func TestRole(t *testing.T) {
	if RoleFor("octo-admin", []string{"Octo-Admin"}) != "admin" {
		t.Error("case-insensitive admin")
	}
	if RoleFor("octo-viewer", []string{"Octo-Admin"}) != "viewer" {
		t.Error("viewer")
	}
	if RoleFor("x", nil) != "viewer" {
		t.Error("nil admins")
	}
}
```
`internal/auth/session_test.go`

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/config"
)

// These tests are regression guards for D-03 (sliding idle expiry) and D-14
// (absolute lifetime). scs v2.9.0 Load marks a found session Modified whenever
// IdleTimeout > 0, so every authenticated request re-commits it and no explicit
// touch is needed (found in 01-07; RESEARCH Pitfall 1 was corrected).

type sessionHarness struct {
	mr *miniredis.Miniredis
	h  http.Handler
}

func newSessionHarness(t *testing.T, idle, lifetime time.Duration) *sessionHarness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{SessionIdleTimeout: idle, SessionLifetime: lifetime}
	sm := NewSessionManager(rdb, cfg)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(RequireSession(api, sm))
	mux.HandleFunc("POST /test/login", func(w http.ResponseWriter, r *http.Request) {
		_ = sm.RenewToken(r.Context())
		sm.Put(r.Context(), KeyUserID, int64(42))
	})
	huma.Register(api, huma.Operation{
		OperationID: "secure", Method: http.MethodGet, Path: "/secure",
		Security: []map[string][]string{{"session": {}}},
	}, func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	huma.Register(api, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/logout",
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, sm.Destroy(ctx)
	})
	return &sessionHarness{mr: mr, h: sm.LoadAndSave(mux)}
}

func (s *sessionHarness) do(method, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in response", CookieName)
	return nil
}

func (s *sessionHarness) login(t *testing.T) *http.Cookie {
	t.Helper()
	return sessionCookie(t, s.do(http.MethodPost, "/test/login", nil))
}

func approx(t *testing.T, name string, got, want, tol time.Duration) {
	t.Helper()
	if got < want-tol || got > want+tol {
		t.Fatalf("%s = %v, want about %v (+/- %v)", name, got, want, tol)
	}
}

func TestSessionCookieFlags(t *testing.T) {
	s := newSessionHarness(t, 10*time.Second, time.Hour)
	c := s.login(t)
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.MaxAge <= 0 || c.Expires.IsZero() {
		t.Errorf("cookie must be persistent, got MaxAge=%d Expires=%v", c.MaxAge, c.Expires)
	}
	approx(t, "MaxAge", time.Duration(c.MaxAge)*time.Second, 10*time.Second, 2*time.Second)
}

func TestSessionSlides(t *testing.T) {
	s := newSessionHarness(t, 10*time.Second, time.Hour)
	c := s.login(t)
	keys := s.mr.Keys()
	if len(keys) != 1 {
		t.Fatalf("want 1 session key, got %v", keys)
	}
	approx(t, "initial TTL", s.mr.TTL(keys[0]), 10*time.Second, time.Second)

	s.mr.FastForward(6 * time.Second)
	approx(t, "TTL after 6s", s.mr.TTL(keys[0]), 4*time.Second, time.Second)

	rec := s.do(http.MethodGet, "/secure", c)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("secured request = %d, want 2xx", rec.Code)
	}
	approx(t, "TTL after request", s.mr.TTL(keys[0]), 10*time.Second, time.Second)
	if next := sessionCookie(t, rec); next.MaxAge <= 0 {
		t.Errorf("refreshed cookie MaxAge = %d, want > 0", next.MaxAge)
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	s := newSessionHarness(t, 10*time.Second, time.Hour)
	c := s.login(t)
	if rec := s.do(http.MethodGet, "/secure", c); rec.Code >= 300 {
		t.Fatalf("fresh session = %d, want 2xx", rec.Code)
	}
	s.mr.FastForward(11 * time.Second)
	if rec := s.do(http.MethodGet, "/secure", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("idle session = %d, want 401", rec.Code)
	}
}

func TestSessionAbsoluteLifetime(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time")
	}
	// scs stores min(idle, remaining lifetime) as the Redis expiry, so activity
	// can never push a session past Lifetime. miniredis only ages keys on
	// FastForward, so each real-time sleep is mirrored with a FastForward.
	s := newSessionHarness(t, 3*time.Second, 4*time.Second)
	c := s.login(t)
	start := time.Now()
	var got int
	for i := 0; i < 8; i++ {
		time.Sleep(time.Second)
		s.mr.FastForward(time.Second)
		rec := s.do(http.MethodGet, "/secure", c)
		got = rec.Code
		if got == http.StatusUnauthorized {
			break
		}
		if got >= 300 {
			t.Fatalf("unexpected status %d", got)
		}
	}
	if got != http.StatusUnauthorized {
		t.Fatalf("session still valid after %v despite 4s lifetime", time.Since(start))
	}
	if elapsed := time.Since(start); elapsed < 3500*time.Millisecond {
		t.Errorf("expired after only %v, before the 4s lifetime", elapsed)
	}
}

func TestSessionLogoutDestroys(t *testing.T) {
	s := newSessionHarness(t, 10*time.Second, time.Hour)
	c := s.login(t)
	if len(s.mr.Keys()) != 1 {
		t.Fatalf("want 1 session key after login, got %v", s.mr.Keys())
	}
	if rec := s.do(http.MethodPost, "/logout", c); rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", rec.Code)
	}
	if keys := s.mr.Keys(); len(keys) != 0 {
		t.Errorf("session key still present after logout: %v", keys)
	}
	if rec := s.do(http.MethodGet, "/secure", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("old cookie after logout = %d, want 401", rec.Code)
	}
}

func TestSessionDefaultsFromConfig(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	sm := NewSessionManager(nil, cfg)
	if sm.IdleTimeout != 168*time.Hour {
		t.Errorf("IdleTimeout = %v, want 168h", sm.IdleTimeout)
	}
	if sm.Lifetime != 720*time.Hour {
		t.Errorf("Lifetime = %v, want 720h", sm.Lifetime)
	}
}
```
### 10.2 Integration test harness (Docker required)

`authtest` boots a **real** Postgres and Redis through testcontainers, wires the full API against the fake GitHub, and exposes a cookie-jar client. The integration test drives the whole login: PKCE round trip, org allowlist outcomes, open-redirect attempts, session renewal, logout.

`internal/auth/authtest/env.go`

```go
//go:build integration

// Package authtest builds a full API against real Postgres and Redis
// containers and the fake GitHub, for the OAuth integration tests.
package authtest

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/fakegithub"
	"github.com/ephematool/ephematool/internal/httpapi"
	"github.com/ephematool/ephematool/internal/store"
)

var shared struct {
	once      sync.Once
	pgURL     string
	redisAddr string
	err       error
}

func startContainers() {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:18.6-alpine",
		tcpostgres.WithDatabase("ephematool"),
		tcpostgres.WithUsername("ephematool"),
		tcpostgres.WithPassword("ephematool"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		shared.err = err
		return
	}
	shared.pgURL, shared.err = pg.ConnectionString(ctx, "sslmode=disable")
	if shared.err != nil {
		return
	}
	rc, err := tcredis.Run(ctx, "redis:8.8-alpine")
	if err != nil {
		shared.err = err
		return
	}
	var uri string
	uri, shared.err = rc.ConnectionString(ctx)
	if shared.err != nil {
		return
	}
	opt, err := redis.ParseURL(uri)
	if err != nil {
		shared.err = err
		return
	}
	shared.redisAddr = opt.Addr
}

// Record is a captured slog record.
type Record struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

// LogCapture is a slog.Handler that keeps every record.
type LogCapture struct {
	mu   sync.Mutex
	recs []Record
}

func (l *LogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (l *LogCapture) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *LogCapture) WithGroup(string) slog.Handler            { return l }
func (l *LogCapture) Handle(_ context.Context, r slog.Record) error {
	rec := Record{Level: r.Level, Msg: r.Message, Attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool { rec.Attrs[a.Key] = a.Value.Any(); return true })
	l.mu.Lock()
	l.recs = append(l.recs, rec)
	l.mu.Unlock()
	return nil
}

// Find returns the records with the given message.
func (l *LogCapture) Find(msg string) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Record
	for _, r := range l.recs {
		if r.Msg == msg {
			out = append(out, r)
		}
	}
	return out
}

// Env is one running API plus its dependencies.
type Env struct {
	Cfg     config.Config
	Server  *httptest.Server
	Fake    *fakegithub.Server
	Client  *http.Client // cookie jar, never follows redirects
	Redis   *redis.Client
	Pool    *pgxpool.Pool
	Queries *store.Queries
	Logs    *LogCapture
}

// NewEnv starts the API wired to the shared containers (reset per call) and a
// fresh fake GitHub. mutate may adjust the config before the API is built.
func NewEnv(t *testing.T, mutate ...func(*config.Config)) *Env {
	t.Helper()
	shared.once.Do(startContainers)
	if shared.err != nil {
		t.Skipf("containers unavailable: %v", shared.err)
	}
	ctx := context.Background()

	pool, err := store.Open(ctx, shared.pgURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "truncate users"); err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: shared.redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	fake := fakegithub.New()
	fakeSrv := httptest.NewServer(fake)
	t.Cleanup(fakeSrv.Close)

	srv := httptest.NewUnstartedServer(nil)
	cfg := config.Config{
		PublicBaseURL:      "http://" + srv.Listener.Addr().String(),
		DatabaseURL:        shared.pgURL,
		GitHubClientID:     fakegithub.ClientID,
		GitHubClientSecret: fakegithub.ClientSecret,
		GitHubAuthorizeURL: fakeSrv.URL + "/login/oauth/authorize",
		GitHubTokenURL:     fakeSrv.URL + "/login/oauth/access_token",
		GitHubAPIURL:       fakeSrv.URL + "/",
		AllowedOrgs:        []string{fakegithub.Org},
		AllowedTeams:       []string{fakegithub.Team},
		AdminLogins:        []string{"Octo-Admin"},
		SessionIdleTimeout: 168 * time.Hour,
		SessionLifetime:    720 * time.Hour,
	}
	for _, m := range mutate {
		m(&cfg)
	}

	sm := auth.NewSessionManager(rdb, cfg)
	q := store.New(pool)
	handler, _ := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Redis:    rdb,
		Sessions: sm,
		Queries:  q,
		Auth:     auth.NewHandlers(cfg, sm, q),
	})
	srv.Config.Handler = handler
	srv.Start()
	t.Cleanup(srv.Close)

	logs := &LogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prev) })

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Env{Cfg: cfg, Server: srv, Fake: fake, Client: client, Redis: rdb, Pool: pool, Queries: q, Logs: logs}
}

// Get issues a GET against the API (or any absolute URL).
func (e *Env) Get(t *testing.T, target string) *http.Response {
	t.Helper()
	if len(target) > 0 && target[0] == '/' {
		target = e.Server.URL + target
	}
	resp, err := e.Client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// Post issues a POST with the same Origin as the API (same-origin).
func (e *Env) Post(t *testing.T, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.Server.URL+path, nil)
	req.Header.Set("Origin", e.Server.URL)
	resp, err := e.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// StartLogin calls the login endpoint and returns the authorize URL it
// redirects to.
func (e *Env) StartLogin(t *testing.T, next string) string {
	t.Helper()
	path := "/api/auth/github/login"
	if next != "" {
		path += "?next=" + url.QueryEscape(next)
	}
	resp := e.Get(t, path)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status %d, want 302", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// Authorize approves the authorize URL as the given fixture login and returns
// the callback URL GitHub would redirect the browser to.
func (e *Env) Authorize(t *testing.T, authorizeURL, login string) string {
	t.Helper()
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("login", login)
	u.RawQuery = q.Encode()
	// Plain client: the fake GitHub host must not see API cookies.
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: status %d, want 302", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// Login drives login -> authorize -> callback and returns the callback response.
func (e *Env) Login(t *testing.T, login, next string) *http.Response {
	t.Helper()
	return e.Get(t, e.Authorize(t, e.StartLogin(t, next), login))
}

// HasSessionCookie reports whether the jar holds a session cookie.
func (e *Env) HasSessionCookie() bool {
	u, _ := url.Parse(e.Server.URL)
	for _, c := range e.Client.Jar.Cookies(u) {
		if c.Name == "ephematool_session" {
			return true
		}
	}
	return false
}
```
`internal/auth/oauth_integration_test.go`

```go
//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/ephematool/ephematool/internal/auth/authtest"
	"github.com/ephematool/ephematool/internal/config"
)

type meBody struct {
	Login     string   `json:"login"`
	AvatarURL string   `json:"avatarUrl"`
	Role      string   `json:"role"`
	Orgs      []string `json:"orgs"`
}

func getMe(t *testing.T, e *authtest.Env) (int, meBody) {
	t.Helper()
	resp := e.Get(t, "/api/me")
	var b meBody
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, b
}

func TestOAuthAllowedMember(t *testing.T) {
	e := authtest.NewEnv(t)
	resp := e.Login(t, "octo-admin", "/")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "ephematool_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 && cookie.Expires.IsZero() {
		t.Fatalf("session cookie wrong: %+v", cookie)
	}
	status, me := getMe(t, e)
	if status != 200 || me.Role != "admin" || me.Login != "octo-admin" {
		t.Fatalf("me: %d %+v", status, me)
	}
	u, err := e.Queries.GetUserByGithubID(t.Context(), 1001)
	if err != nil || u.Login != "octo-admin" {
		t.Fatalf("users row: %+v %v", u, err)
	}
	if len(e.Logs.Find("auth.login")) != 1 {
		t.Fatal("expected one auth.login event")
	}
}

func TestOAuthViewerRole(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Login(t, "octo-viewer", "/")
	if status, me := getMe(t, e); status != 200 || me.Role != "viewer" {
		t.Fatalf("me: %d %+v", status, me)
	}
}

func TestOAuthTeamMember(t *testing.T) {
	e := authtest.NewEnv(t)
	resp := e.Login(t, "octo-teamonly", "/")
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("location %q", resp.Header.Get("Location"))
	}
	status, me := getMe(t, e)
	if status != 200 || len(me.Orgs) != 1 || me.Orgs[0] != "partner-org/sre" {
		t.Fatalf("me: %d %+v", status, me)
	}
}

func TestOAuthDeniedNonMember(t *testing.T) {
	for _, login := range []string{"octo-outsider", "octo-pending", "octo-forbidden"} {
		t.Run(login, func(t *testing.T) {
			e := authtest.NewEnv(t)
			resp := e.Login(t, login, "/")
			want := "/denied?reason=org_membership&login=" + url.QueryEscape(login)
			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
				t.Fatalf("callback: %d %q, want %q", resp.StatusCode, resp.Header.Get("Location"), want)
			}
			if e.HasSessionCookie() {
				t.Fatal("denied login must not get a session cookie")
			}
			if len(e.Logs.Find("auth.denied")) != 1 {
				t.Fatal("expected auth.denied event")
			}
			if login == "octo-forbidden" && len(e.Logs.Find("auth.org_check_forbidden")) == 0 {
				t.Fatal("expected auth.org_check_forbidden event")
			}
			if status, _ := getMe(t, e); status != 401 {
				t.Fatalf("me: %d, want 401", status)
			}
		})
	}
}

func TestOAuthStateMismatch(t *testing.T) {
	e := authtest.NewEnv(t)
	cb := e.Authorize(t, e.StartLogin(t, "/"), "octo-admin")
	u, _ := url.Parse(cb)
	q := u.Query()
	q.Set("state", "tampered")
	u.RawQuery = q.Encode()
	resp := e.Get(t, u.String())
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?reason=failed" {
		t.Fatalf("callback: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if e.HasSessionCookie() {
		t.Fatal("no session expected")
	}
	if status, _ := getMe(t, e); status != 401 {
		t.Fatalf("me: %d", status)
	}
}

func TestOAuthPKCESent(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Authorize(t, e.StartLogin(t, "/"), "octo-viewer")
	q := e.Fake.LastAuthorize()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("PKCE params missing: %v", q)
	}
	if q.Get("scope") != "read:org" {
		t.Fatalf("scope %q, want read:org", q.Get("scope"))
	}
}

func TestOAuthOpenRedirect(t *testing.T) {
	for _, next := range []string{"//evil.example", "https://evil.example", `/\evil`} {
		t.Run(next, func(t *testing.T) {
			e := authtest.NewEnv(t)
			resp := e.Login(t, "octo-viewer", next)
			if resp.Header.Get("Location") != "/" {
				t.Fatalf("next %q landed on %q", next, resp.Header.Get("Location"))
			}
		})
	}
}

func TestOAuthNextPreserved(t *testing.T) {
	e := authtest.NewEnv(t)
	resp := e.Login(t, "octo-viewer", "/status?x=1")
	if resp.Header.Get("Location") != "/status?x=1" {
		t.Fatalf("location %q", resp.Header.Get("Location"))
	}
}

func TestOAuthCancelled(t *testing.T) {
	e := authtest.NewEnv(t)
	u, _ := url.Parse(e.StartLogin(t, "/"))
	q := u.Query()
	q.Set("cancel", "1")
	u.RawQuery = q.Encode()
	// Reuse the helper path: authorize with cancel returns the error callback.
	resp, err := http.DefaultTransport.RoundTrip(mustReq(t, u.String()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cb := e.Get(t, resp.Header.Get("Location"))
	if cb.StatusCode != http.StatusFound || cb.Header.Get("Location") != "/login?reason=failed" {
		t.Fatalf("callback: %d %q", cb.StatusCode, cb.Header.Get("Location"))
	}
}

func mustReq(t *testing.T, target string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOAuthNotConfigured(t *testing.T) {
	e := authtest.NewEnv(t, func(c *config.Config) { c.GitHubClientID = ""; c.GitHubClientSecret = "" })
	resp := e.Get(t, "/api/auth/config")
	var body struct {
		GitHubConfigured bool     `json:"githubConfigured"`
		AllowedOrgs      []string `json:"allowedOrgs"`
		AllowedTeams     []string `json:"allowedTeams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.GitHubConfigured {
		t.Fatal("githubConfigured must be false")
	}
	login := e.Get(t, "/api/auth/github/login")
	if login.StatusCode != http.StatusFound || login.Header.Get("Location") != "/login?reason=failed" {
		t.Fatalf("login: %d %q", login.StatusCode, login.Header.Get("Location"))
	}
	if len(e.Logs.Find("auth.github_not_configured")) == 0 {
		t.Fatal("expected auth.github_not_configured event")
	}
}

func TestLogout(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Login(t, "octo-viewer", "/")
	if n := len(e.Redis.Keys(t.Context(), "scs:session:*").Val()); n != 1 {
		t.Fatalf("expected 1 session key, got %d", n)
	}
	if resp := e.Post(t, "/api/auth/logout"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if status, _ := getMe(t, e); status != 401 {
		t.Fatalf("me after logout: %d", status)
	}
	if n := len(e.Redis.Keys(t.Context(), "scs:session:*").Val()); n != 0 {
		t.Fatalf("session key not removed: %d", n)
	}
}
```
---

## 11. `internal/httpapi`

The API is a Huma router on `net/http`. Layer order, outermost first:

`CrossOriginProtection` → `otelhttp` → `TraceIDHeader` → `sessions.LoadAndSave` → `mux`

- **CrossOriginProtection** (stdlib) blocks cross-site state-changing requests by `Origin` / `Sec-Fetch-Site`. nginx forwards those headers unchanged.
- **Route tagging** names every server span `METHOD /route/{template}` and adds `http.route`, so metrics have bounded cardinality (Pitfall 2). scs hands the mux a *copy* of the request, so Go's `Request.Pattern` is unavailable to `otelhttp`; tagging from the Huma operation (and `RouteHandler` for plain handlers) is the workaround, and `middleware_test.go` proves it.
- **TraceIDHeader** returns `X-Trace-Id` on every response so the UI can show "view trace" links.

`internal/httpapi/middleware.go`

```go
package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// tagRoute names the active server span after the route template and adds the
// bounded http.route attribute to both the span and the otelhttp metrics
// labeler. Raw request paths must never be used (cardinality, Pitfall 2).
func tagRoute(r *http.Request, method, route string) *http.Request {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	span.SetName(method + " " + route)
	span.SetAttributes(semconv.HTTPRoute(route))
	if l, ok := otelhttp.LabelerFromContext(ctx); ok {
		l.Add(semconv.HTTPRoute(route))
	}
	return r
}

// RouteTagging is a Huma middleware that tags the server span and metrics with
// the operation's route template.
func RouteTagging(ctx huma.Context, next func(huma.Context)) {
	if op := ctx.Operation(); op != nil {
		c := ctx.Context()
		span := trace.SpanFromContext(c)
		span.SetName(op.Method + " " + op.Path)
		span.SetAttributes(semconv.HTTPRoute(op.Path))
		if l, ok := otelhttp.LabelerFromContext(c); ok {
			l.Add(semconv.HTTPRoute(op.Path))
		}
	}
	next(ctx)
}

// RouteHandler tags non-Huma handlers (for example the OAuth routes) with their
// route template.
func RouteHandler(method, pattern string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, tagRoute(r, method, pattern))
	})
}

// TraceIDHeader exposes the active trace id as X-Trace-Id so the UI and
// operators can look the request up in Tempo.
func TraceIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			w.Header().Set("X-Trace-Id", sc.TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
}
```
`internal/httpapi/status.go`

```go
package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/status"
)

// ServiceStatus is one row of the telemetry status page, filled from the
// prober's cached snapshot.
type ServiceStatus struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Group        string     `json:"group" enum:"platform,data,pipeline,backends"`
	Status       string     `json:"status" enum:"healthy,degraded,down,unknown"`
	LastSignalAt *time.Time `json:"lastSignalAt,omitempty"`
	Detail       string     `json:"detail"`
}

// BuildSection groups per-component build metadata.
type BuildSection struct {
	API buildinfo.BuildInfo `json:"api"`
}

// StatusBody is the /api/status payload.
type StatusBody struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	GrafanaURL  string          `json:"grafanaUrl"`
	Build       BuildSection    `json:"build"`
	Services    []ServiceStatus `json:"services"`
}

// StatusResponse is the Huma output wrapper.
type StatusResponse struct {
	Body StatusBody
}

const pingThrottleKey = "telemetry:ping:throttle"

func registerStatus(api huma.API, deps Deps) {
	huma.Get(api, "/api/status", func(ctx context.Context, _ *struct{}) (*StatusResponse, error) {
		enqueued := maybeEnqueuePing(ctx, deps)
		slog.InfoContext(ctx, "status.served", "ping_enqueued", enqueued)
		generatedAt, services := time.Now().UTC(), []ServiceStatus{}
		if deps.Status != nil {
			generatedAt, services = snapshotServices(deps.Status)
		}
		return &StatusResponse{Body: StatusBody{
			GeneratedAt: generatedAt,
			GrafanaURL:  deps.Config.GrafanaPublicURL,
			Build:       BuildSection{API: buildinfo.Info()},
			Services:    services,
		}}, nil
	}, func(o *huma.Operation) {
		o.OperationID = "get-status"
		o.Summary = "Telemetry status snapshot"
		// RequireSession answers 401 without a signed-in user (T-01-28).
		o.Security = []map[string][]string{{"session": {}}}
	})
}

// snapshotServices maps the cached prober snapshot to the API model. Before the
// first probe completes the generation time is zero; report "now" instead.
func snapshotServices(p *status.Prober) (time.Time, []ServiceStatus) {
	at, results := p.Snapshot()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	out := make([]ServiceStatus, 0, len(results))
	for i, r := range results {
		out = append(out, ServiceStatus{
			ID:           r.ID,
			Name:         status.Services[i].Name,
			Group:        status.Services[i].Group,
			Status:       r.Status,
			LastSignalAt: r.LastSignalAt,
			Detail:       r.Detail,
		})
	}
	return at, out
}

// maybeEnqueuePing enqueues the ping task unless throttled. Failures are logged
// and never fail the request.
func maybeEnqueuePing(ctx context.Context, deps Deps) bool {
	enq := deps.enqueuer()
	if enq == nil {
		return false
	}
	if ttl := deps.Config.PingThrottle; ttl > 0 && deps.Redis != nil {
		ok, err := deps.Redis.SetNX(ctx, pingThrottleKey, 1, ttl).Result()
		if err != nil {
			slog.WarnContext(ctx, "status.ping_throttle_failed", "error", err)
			return false
		}
		if !ok {
			return false
		}
	}
	if err := enq.EnqueuePing(ctx); err != nil {
		slog.WarnContext(ctx, "status.ping_enqueue_failed", "error", err)
		return false
	}
	return true
}
```
`internal/httpapi/api.go`

```go
// Package httpapi wires the Huma API, route-accurate telemetry and handlers.
package httpapi

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/status"
	"github.com/ephematool/ephematool/internal/store"
)

// Enqueuer publishes the telemetry ping task. *queue.Client satisfies it.
type Enqueuer interface {
	EnqueuePing(ctx context.Context) error
}

// Deps are the collaborators handlers need. Later plans add fields.
type Deps struct {
	Config config.Config
	Redis  *redis.Client
	Queue  *queue.Client
	// Enqueuer overrides Queue when set (tests).
	Enqueuer Enqueuer
	// Sessions, Queries and Auth are optional; Auth is built from the others
	// when nil. All may be nil when only the OpenAPI spec is generated.
	Sessions *scs.SessionManager
	Queries  *store.Queries
	Auth     *auth.Handlers
	// Status is the background prober whose cached snapshot backs /api/status.
	Status *status.Prober
}

func (d Deps) enqueuer() Enqueuer {
	if d.Enqueuer != nil {
		return d.Enqueuer
	}
	if d.Queue != nil {
		return d.Queue
	}
	return nil
}

// New builds the HTTP handler and the Huma API. Layer order, outermost first:
// CrossOriginProtection -> otelhttp -> TraceIDHeader -> sessions.LoadAndSave -> mux.
func New(deps Deps) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Ephematool API", buildinfo.Version))
	api.UseMiddleware(RouteTagging)
	if deps.Auth == nil {
		deps.Auth = auth.NewHandlers(deps.Config, deps.Sessions, deps.Queries)
	}
	auth.Register(api, mux, deps.Auth, RouteHandler)
	registerStatus(api, deps)

	var inner http.Handler = mux
	if deps.Sessions != nil {
		inner = deps.Sessions.LoadAndSave(mux)
	}
	h := otelhttp.NewHandler(TraceIDHeader(inner), "ephematool-api")
	return http.NewCrossOriginProtection().Handler(h), api
}
```
`internal/httpapi/middleware_test.go`

```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/config"
)

type itemInput struct {
	ID string `path:"id"`
}

type itemOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

// copyRequestMW simulates scs LoadAndSave, which hands the mux a request copy
// (r.WithContext) so otelhttp never sees Request.Pattern (Pitfall 2).
func copyRequestMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), struct{}{}, "x")))
	})
}

type harness struct {
	handler http.Handler
	spans   *tracetest.InMemoryExporter
	reader  *sdkmetric.ManualReader
}

func newHarness(t *testing.T, register func(api huma.API)) harness {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(RouteTagging)
	register(api)

	h := otelhttp.NewHandler(TraceIDHeader(copyRequestMW(mux)), "ephematool-api",
		otelhttp.WithTracerProvider(tp), otelhttp.WithMeterProvider(mp))
	return harness{handler: h, spans: exp, reader: reader}
}

func TestRouteTagging(t *testing.T) {
	h := newHarness(t, func(api huma.API) {
		huma.Get(api, "/api/items/{id}", func(ctx context.Context, in *itemInput) (*itemOutput, error) {
			out := &itemOutput{}
			out.Body.ID = in.ID
			return out, nil
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/items/42", nil)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	spans := h.spans.GetSpans()
	require.NotEmpty(t, spans)
	var found bool
	for _, s := range spans {
		if s.Name == "GET /api/items/{id}" {
			found = true
			var route string
			for _, a := range s.Attributes {
				if a.Key == "http.route" {
					route = a.Value.AsString()
				}
			}
			assert.Equal(t, "/api/items/{id}", route)
		}
		assert.NotContains(t, s.Name, "/42")
	}
	require.True(t, found, "span named 'GET /api/items/{id}' not recorded; got %v", spanNames(spans))

	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	var routes []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range hist.DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key("http.route")); ok {
					routes = append(routes, v.AsString())
				}
			}
		}
	}
	assert.Equal(t, []string{"/api/items/{id}"}, routes)
}

func TestTraceIDHeader(t *testing.T) {
	h := newHarness(t, func(api huma.API) {
		huma.Get(api, "/api/items/{id}", func(ctx context.Context, in *itemInput) (*itemOutput, error) {
			return &itemOutput{}, nil
		})
	})
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/items/1", nil))

	got := rec.Header().Get("X-Trace-Id")
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), got)
	var server string
	for _, s := range h.spans.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			server = s.SpanContext.TraceID().String()
		}
	}
	assert.Equal(t, server, got)
}

type fakeEnqueuer struct {
	mu    sync.Mutex
	calls []context.Context
}

func (f *fakeEnqueuer) EnqueuePing(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ctx)
	return nil
}

func TestStatusEnqueuesPing(t *testing.T) {
	fe := &fakeEnqueuer{}
	deps := Deps{
		Config:   config.Config{GrafanaPublicURL: "http://localhost:3000", PingThrottle: 0},
		Enqueuer: fe,
	}
	h := newHarness(t, func(api huma.API) { registerStatus(api, deps) })

	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body, "generatedAt")
	assert.Equal(t, "http://localhost:3000", body["grafanaUrl"])
	assert.Contains(t, body, "services")
	build := body["build"].(map[string]any)
	api := build["api"].(map[string]any)
	assert.Contains(t, api, "version")

	require.Len(t, fe.calls, 1)
	var server trace.SpanContext
	for _, s := range h.spans.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			server = s.SpanContext
		}
	}
	got := trace.SpanContextFromContext(fe.calls[0])
	require.True(t, got.IsValid())
	assert.Equal(t, server.TraceID(), got.TraceID())
	assert.Equal(t, server.SpanID(), got.SpanID(), "enqueue ctx must carry the server span so the producer span is its child")
}

func spanNames(spans tracetest.SpanStubs) []string {
	var n []string
	for _, s := range spans {
		n = append(n, s.Name)
	}
	return n
}
```
---

## 12. Binaries and BUILD files

### 12.1 `cmd/api/main.go`

Three subcommands: `serve` (default), `migrate` (one-shot, used by tooling), and `openapi` (prints the OpenAPI 3.1 document; this is how `api/openapi.json` is generated). Note what the Redis instrumentation does **not** record: `WithDBStatement(false)`, because scs session keys contain the session token and the stored value is the serialised session.

`cmd/api/main.go`

```go
package main

//go:generate sh -c "go run . openapi > ../../api/openapi.json"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/httpapi"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/status"
	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/internal/telemetry"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	case "migrate":
		if err := migrate(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	case "openapi":
		if err := printOpenAPI(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (serve|migrate|openapi)\n", cmd)
		os.Exit(2)
	}
}

func printOpenAPI() error {
	_, api := httpapi.New(httpapi.Deps{})
	b, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, buf.String())
	return err
}

func migrate() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return store.Migrate(ctx, pool)
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "ephematool-api",
		Version:     buildinfo.Version,
		Env:         cfg.AppEnv,
	})
	if err != nil {
		return err
	}
	defer func() {
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdownTelemetry(fctx)
	}()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	// Do not record Redis command text: scs session keys embed the session token
	// and the SET value is the serialized session, so db.statement would leak both.
	if err := redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false)); err != nil {
		return err
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return err
	}
	defer rdb.Close()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if cfg.MigrateOnStart {
		if err := store.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	queries := store.New(pool)
	sessions := auth.NewSessionManager(rdb, cfg)

	q := queue.NewClient(cfg.RedisAddr)
	defer q.Close()

	prober, closeProbe, err := newProber(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeProbe()
	go prober.Run(ctx)

	handler, _ := httpapi.New(httpapi.Deps{
		Config: cfg, Redis: rdb, Queue: q, Status: prober,
		Sessions: sessions, Queries: queries, Auth: auth.NewHandlers(cfg, sessions, queries),
	})
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "api.listening", "addr", cfg.HTTPAddr, "version", buildinfo.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.InfoContext(sctx, "api.shutting_down")
		return srv.Shutdown(sctx)
	}
	return nil
}

// newProber builds the status prober. It owns uninstrumented Postgres and Redis
// connections so the probes do not create a trace every cycle.
func newProber(ctx context.Context, cfg config.Config) (*status.Prober, func(), error) {
	probePool, err := store.OpenUntraced(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open probe pool: %w", err)
	}
	probeRedis := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})

	checks := status.Checks{
		Postgres: func(c context.Context) error { return probePool.Ping(c) },
		Redis:    func(c context.Context) error { return probeRedis.Ping(c).Err() },
		HeartbeatAt: func(c context.Context) (time.Time, error) {
			v, err := probeRedis.Get(c, queue.HeartbeatKey).Result()
			if err != nil {
				return time.Time{}, err
			}
			secs, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(secs, 0), nil
		},
		HTTP: map[string]string{
			"dashboard":      cfg.WebHealthURL,
			"otel-collector": cfg.CollectorHealthURL,
			"tempo":          cfg.TempoReadyURL,
			"loki":           cfg.LokiReadyURL,
			"prometheus":     cfg.PrometheusReadyURL,
			"grafana":        cfg.GrafanaHealthURL,
		},
		LastSignal: status.NewLastSignal(cfg.PrometheusURL, cfg.ProbeTimeout),
	}
	p := status.NewProber(checks, cfg.ProbeInterval, cfg.ProbeTimeout, otel.Meter("ephematool/status"))
	return p, func() { probePool.Close(); _ = probeRedis.Close() }, nil
}
```
### 12.2 `cmd/worker/main.go`

`cmd/worker/main.go`

```go
// Command worker consumes asynq tasks and emits its own telemetry.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "ephematool-worker",
		Version:     buildinfo.Version,
		Env:         cfg.AppEnv,
	})
	if err != nil {
		return err
	}
	defer func() {
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdownTelemetry(fctx)
	}()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	// Same rule as the API: never record Redis command text in spans.
	if err := redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false)); err != nil {
		return err
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return err
	}
	defer rdb.Close()

	srv := queue.NewServer(cfg.RedisAddr)
	sched, err := queue.NewScheduler(cfg.RedisAddr, cfg.HeartbeatInterval)
	if err != nil {
		return err
	}
	if err := sched.Start(); err != nil {
		return err
	}
	if err := srv.Start(queue.NewMux(rdb)); err != nil {
		return err
	}
	slog.InfoContext(ctx, "worker started", "version", buildinfo.Version)

	<-ctx.Done()
	slog.InfoContext(context.Background(), "worker shutting down")
	srv.Shutdown()
	sched.Shutdown()
	return nil
}
```
### 12.3 Hand-written BUILD files

Run Gazelle first, then overwrite these five with the versions below (they add codegen rules Gazelle cannot infer):

```bash
bazel run //:gazelle
```

`api/BUILD.bazel`

```python
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")

# OpenAPI 3.1 document emitted by the API binary (`api openapi`); replaces the go:generate
# directive in cmd/api/main.go for the Bazel build.
genrule(
    name = "openapi_gen",
    outs = ["_gen/openapi.json"],
    cmd = "$(execpath //cmd/api) openapi > $@",
    tools = ["//cmd/api"],
    visibility = ["//:__subpackages__"],
)

write_source_files(
    name = "gen",
    files = {"openapi.json": "_gen/openapi.json"},
    suggested_update_target = "//:gen",
    visibility = ["//:__subpackages__"],
)
```
`conventions/BUILD.bazel`

```python
# D-09 conventions source of truth; generated into Go and TS by //tools/genconventions.
exports_files(
    ["telemetry.json"],
    visibility = ["//:__subpackages__"],
)
```
`internal/conventions/BUILD.bazel`

```python
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("//tools/bzl:go.bzl", "go_test")

go_library(
    name = "conventions",
    srcs = [
        "conventions.gen.go",
        "generate.go",
    ],
    importpath = "github.com/ephematool/ephematool/internal/conventions",
    visibility = ["//:__subpackages__"],
)

go_test(
    name = "conventions_test",
    srcs = ["conventions_test.go"],
    embed = [":conventions"],
    deps = ["@com_github_stretchr_testify//assert"],
)

# Codegen (replaces the go:generate directive in generate.go for the Bazel build).
# Writes the Go constants here and the TS constants consumed by //web:gen.
genrule(
    name = "conventions_gen",
    srcs = ["//conventions:telemetry.json"],
    outs = [
        "_gen/conventions.gen.go",
        "_gen/conventions.gen.ts",
    ],
    cmd = "$(execpath //tools/genconventions) -in $(execpath //conventions:telemetry.json) -go-out $(execpath _gen/conventions.gen.go) -ts-out $(execpath _gen/conventions.gen.ts)",
    tools = ["//tools/genconventions"],
    visibility = ["//:__subpackages__"],
)

write_source_files(
    name = "gen",
    files = {"conventions.gen.go": "_gen/conventions.gen.go"},
    suggested_update_target = "//:gen",
    visibility = ["//:__subpackages__"],
)
```
`internal/store/BUILD.bazel`

```python
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "store",
    srcs = [
        "db.go",
        "generate.go",
        "models.go",
        "store.go",
        "users.sql.go",
    ],
    embedsrcs = ["migrations/00001_users.sql"],
    importpath = "github.com/ephematool/ephematool/internal/store",
    visibility = ["//:__subpackages__"],
    deps = [
        "@com_github_exaring_otelpgx//:otelpgx",
        "@com_github_jackc_pgx_v5//:pgx",
        "@com_github_jackc_pgx_v5//pgconn",
        "@com_github_jackc_pgx_v5//pgtype",
        "@com_github_jackc_pgx_v5//pgxpool",
        "@com_github_jackc_pgx_v5//stdlib",
        "@com_github_pressly_goose_v3//:goose",
    ],
)

# sqlc codegen (replaces the go:generate directive in generate.go for the Bazel build).
# sqlc comes from the go.mod `tool` directive. The repo-root sqlc.yaml stays the source of
# truth; only its `out` path is rewritten so sqlc writes to _gen/ instead of the source tree.
_SQLC_QUERY_OUTS = ["_gen/%s.go" % q.removeprefix("queries/") for q in glob(["queries/*.sql"])]

_SQLC_OUTS = [
    "_gen/db.go",
    "_gen/models.go",
] + _SQLC_QUERY_OUTS

genrule(
    name = "sqlc_gen",
    srcs = [
        "//:sqlc.yaml",
    ] + glob([
        "migrations/*.sql",
        "queries/*.sql",
    ]),
    outs = _SQLC_OUTS,
    cmd = """
set -euo pipefail
# sqlc resolves paths relative to the config file, so the rewritten config sits at the
# execroot (next to the sandboxed inputs) and only `out` is redirected to _gen/.
cfg="sqlc.$$$$.bazel.yaml"
sed -e "s#out: .*#out: $(RULEDIR)/_gen#" $(execpath //:sqlc.yaml) > "$$cfg"
$(execpath @com_github_sqlc_dev_sqlc//cmd/sqlc) generate -f "$$cfg"
rm -f "$$cfg"
""",
    tools = ["@com_github_sqlc_dev_sqlc//cmd/sqlc"],
)

write_source_files(
    name = "gen",
    files = {out.removeprefix("_gen/"): out for out in _SQLC_OUTS},
    suggested_update_target = "//:gen",
    visibility = ["//:__subpackages__"],
)
```
`tools/genconventions/BUILD.bazel`

```python
load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")
load("//tools/bzl:go.bzl", "go_test")

go_library(
    name = "genconventions_lib",
    srcs = ["main.go"],
    importpath = "github.com/ephematool/ephematool/tools/genconventions",
    visibility = ["//visibility:private"],
)

go_binary(
    name = "genconventions",
    embed = [":genconventions_lib"],
    visibility = ["//visibility:public"],
)

go_test(
    name = "genconventions_test",
    srcs = ["main_test.go"],
    embed = [":genconventions_lib"],
    deps = [
        "@com_github_stretchr_testify//assert",
        "@com_github_stretchr_testify//require",
    ],
)
```
Then generate the committed generated files and the OpenAPI document:

```bash
bazel run //internal/conventions:gen
bazel run //internal/store:gen
bazel run //api:gen
```

`bazel run //api:gen` builds `//cmd/api`, runs `api openapi`, and writes `api/openapi.json`. Phase 1c turns that document into `web/src/api/schema.d.ts`.

---

## 13. Build and test

```bash
bazel build //cmd/... //internal/...
bazel test //internal/... //tools/... --test_tag_filters=-integration
# Docker required (starts Postgres and Redis containers):
bazel test --config=integration //internal/auth:auth_test
```

Expected: all unit tests pass; the integration test passes with Docker running.

## 14. Phase 1a acceptance checklist

- [ ] `bazel build //cmd/...` succeeds and produces `api`, `worker`, `fakegithub`.
- [ ] `bazel test //internal/...` passes (unit tests only, no Docker).
- [ ] `bazel test --config=integration //internal/auth:auth_test` passes.
- [ ] `bazel run //api:gen` produces `api/openapi.json` with operations `get-status`, `get-me`, `get-auth-config`, `post-auth-logout`.
- [ ] You can explain, without looking: why `RouteTagging` exists, why the access token is never stored, why `Evaluate` fails closed, and why the status prober uses an untraced pool.

## 15. Common failures

| Symptom | Cause | Fix |
|---|---|---|
| Test hangs then `docker: permission denied` | integration run inside the sandbox | use `--config=integration` (it disables the sandbox for the test runner). |
| `no such package '@com_github_sqlc_dev_sqlc//cmd/sqlc'` | `go.mod` `tool` directive missing | restore the `tool github.com/sqlc-dev/sqlc/cmd/sqlc` line and run `bazel mod tidy`. |
| Spans named `GET` instead of `GET /api/status` | `RouteTagging` not registered on the Huma API | `api.UseMiddleware(RouteTagging)` before registering operations. |
| `/api/me` returns 401 after a successful login in the browser | cookie `Secure` set over plain HTTP | `COOKIE_SECURE=false` for local, `true` only behind TLS. |
| Callback always redirects to `/login?reason=failed` | `PUBLIC_BASE_URL` does not match the GitHub OAuth App callback URL | the callback is `<PUBLIC_BASE_URL>/api/auth/github/callback`. |
| `go: updates to go.mod needed` | `go.mod` out of sync with imports | `go mod tidy`, then `bazel mod tidy`. |
