# Phase 2a · Foundations: config contract, fake GitHub, audit store, GitHub App client, roles

**Objective:** build everything the create-environment feature stands on, before any feature code exists: one validated configuration contract, a scriptable fake GitHub (OAuth, App tokens, refs, PRs, registry, Actions), the append-only audit store behind two database roles, a GitHub App client, and request-time roles with secure-by-default routes.

**Design references:** `.planning/phases/02-create-a-secure-preview-environment/` `02-CONTEXT.md` (D-06, D-07, D-17, D-19, D-23, D-24), `02-RESEARCH.md` (Patterns 1, 2, 8, 11; "Fake GitHub surface"), `02-PATTERNS.md` (sections 2, 3, 5, 7, 12), `01-telemetry-spine-github-login/01-REVIEW.md` (WR-01, WR-02, WR-03, WR-08, WR-09, WR-10, WR-11).
**Plans covered:** 02-01 (provenance gate), 02-02 (config + conventions), 02-03 (fake GitHub), 02-04 (store + audit), 02-05 (ghapp), 02-06 (roles).
**Requirements covered (foundations only):** ENV-01, ENV-09, OBS-03, AUTH-02 (the create flow itself arrives in 2b).
**Prerequisite:** Phase 1 acceptance checklist complete (`phase-1c-dashboard.md`, section "Phase 1 acceptance"). `bazel test //...` is green and `bazel run //:up_fake` works.
**Produces (later phases rely on these exact names):**

| Artifact | Used later for |
|---|---|
| `config.Config` Phase 2 fields, `PreviewHost()`, `PreviewURL()`, `MigrationURL()`, `GitHubAppConfigured()` | every Phase 2 plan reads settings from this one struct |
| `environments/conventions.gen.env` | collector, kind manifests, `provision.sh`, Helm get label/attribute keys without hand-writing them |
| `fakegithub` App/refs/pulls/registry/Actions routes, `SetTeamMembership`, `AdvanceRun`, `SetRunStep`, `Dispatches` | 2b/2c integration tests, e2e and Playwright |
| `environments`, `audit_events` tables, `ephematool_app` role, CAS queries | create API (2b), worker (2c), audit page (2d) |
| `internal/audit` (`Record`, `List`), `internal/testenv` | create API, audit API, every integration test |
| `internal/ghapp` (`Client`, `ValidateRefInput`, `ImageTag`, `Dispatch`, `GetRun`, `ListJobs`) | create API validation (2b), worker (2c) |
| `auth.RoleResolver`, `RequireRole`, `RequireSession` secure-by-default, `MarkPublic` | all later API routes |

> **How to read this document.** Every file is complete: copy it to the path shown. Files that already exist from Phase 1 are marked **replace** (take the whole new file) or **edit** (the change is shown). Commands are `bash` from the repository root.

---

## 1. What this phase adds

```
.env.example                                  # edit: Phase 2 groups
.gitignore                                    # edit: deploy/.secrets/, .tools/
BUILD.bazel                                   # edit: _GEN_PACKAGES, test_integration
deploy/
├── kind/versions.env                         # new: pinned versions (plan 02-01)
└── compose.yaml                              # edit: api and worker services (role split, Phase 2 env)
environments/
├── BUILD.bazel                               # new
└── conventions.gen.env                       # generated
tools/stack.sh                                # edit: ensure_secrets
scripts/verify.d/conventions.sh               # replace: widened gate
cmd/api/main.go                               # replace: owner/app split, optional App client
cmd/fakegithub/main.go                        # replace: FAKE_RUN_STEP
internal/
├── config/{config.go,validate.go,config_test.go}              # replace
├── conventions/BUILD.bazel                   # edit: -env-out
├── fakegithub/{server.go,app.go,repos.go,registry.go,actions.go}
│                {server_test.go,repos_test.go,actions_test.go}
├── store/
│   ├── store.go                              # replace: locked Provider migrate
│   ├── migrations/{00002_app_role_environments.sql,00003_audit_events.sql}
│   ├── queries/{environments.sql,audit.sql}
│   └── migrate_integration_test.go, helpers_integration_test.go
├── testenv/env.go                            # new: two-role Postgres + Redis harness
├── audit/{audit.go,cursor.go,audit_test.go,cursor_test.go,audit_integration_test.go}
├── ghapp/{ghapp.go,token.go,refs.go,image.go,actions.go,validate.go}
│         {ghapp_test.go,refs_test.go,actions_test.go,validate_test.go}
└── auth/{role.go,middleware.go,handlers.go}                   # replace
         {role_test.go,role_integration_test.go}               # new
internal/httpapi/security_test.go             # new: every operation protected or public
web/src/components/{RoleBadge.tsx,RoleBadge.test.tsx}          # replace/new
```

```bash
mkdir -p environments internal/{audit,ghapp,testenv}
```

---

## 2. Supply-chain gate (plan 02-01)

Phase 2 introduces tools and modules that Phase 1 did not use. **Nothing is installed until this gate is approved.** The goal is one recorded decision, so later steps can say "approved in 2a section 2" instead of asking again.

### 2.1 Pinned versions

`deploy/kind/versions.env` is a shell-sourceable file that every later script reads. Versions, image digests and official checksums were copied from the publishers' own pages on 2026-10-12; each comment names the source.

`deploy/kind/versions.env`

```bash
# Pinned versions, image digests and official checksums for Phase 2 (plan 02-01).
# Values taken from official sources on 2026-10-12. Source URL in the comment above each key.

# kubernetes-sigs/kind v0.33.0 release asset digests (gh api repos/kubernetes-sigs/kind/releases/tags/v0.33.0)
KIND_VERSION=v0.33.0
KIND_SHA256_DARWIN_ARM64=0c8c7dbe5e23594a198b786c4bc13dacc101fa6196b0cb0b23a1ca44e61f4b4f
KIND_SHA256_DARWIN_AMD64=5a99f26f57246dc9319dd294803313197a0f34d33c525b3ea8b655db5916ece0
KIND_SHA256_LINUX_AMD64=aee6151561422756b764a4ae28e7f44cda5af5a9eead3cc9985112b1de8d8e0d
# kind v0.33.0 release notes; digest confirmed via docker buildx imagetools inspect
KIND_NODE_IMAGE=kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed

# https://get.helm.sh/helm-v4.3.0-<os>-<arch>.tar.gz.sha256sum (published values)
HELM_VERSION=v4.3.0
HELM_SHA256_DARWIN_ARM64=d3870437e1e95b67f8edbde964156c84a26503f560821d40c542441658934fba
HELM_SHA256_DARWIN_AMD64=347a784877e0e20eac865e8d1c36a80f6bb0861d6f29abd34defb6570ef95d92
HELM_SHA256_LINUX_AMD64=86584a54def73570558f66f5111cc53dfed56689637ae32c1201205d494f54fb

# Minimum kubectl minor you install yourself
KUBECTL_MIN_MINOR=36

# Docker Hub multi-arch index digests (docker buildx imagetools inspect)
TRAEFIK_IMAGE=traefik:v3.7.14@sha256:575fa15b135078fe5e50aa847987d96dbddd7b093c172429618404df73f3fa7c
# https://traefik.github.io/charts/index.yaml, entry traefik 41.7.1
TRAEFIK_CHART_REPO=https://traefik.github.io/charts
TRAEFIK_CHART_VERSION=41.7.1
TRAEFIK_CHART_DIGEST=8566d98f9636c38ab59098978520a37160fa1e9f0a13af3dde4218529fa21d73
WHOAMI_IMAGE=traefik/whoami:v1.12.0@sha256:c4717a8d1f0134a7444e24f881160e033991f23027c6c5a9a3f8fd22e70d1d44
# busybox:stable (BusyBox v1.36.1) index digest
BUSYBOX_IMAGE=busybox:stable@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662

# actions/runner v2.338.0 release body (gh api repos/actions/runner/releases/tags/v2.338.0)
RUNNER_VERSION=2.338.0
RUNNER_SHA256_OSX_ARM64=df4cebda25c86a886ed204e49fee63f5c2e7cec5f447b5c98440a826bbdf9df2
RUNNER_SHA256_OSX_X64=dea7a58796ce215fc424a8b27c0fdd9b813fa5fc03d109a4a1d38131d6bfa1a3
RUNNER_SHA256_LINUX_X64=af4b794c1bc41d73d40535e3fe092a39f9679cd8d965954c2aca25a05ca41d32

# actions/checkout v7.0.1, dereferenced to a commit (gh api repos/actions/checkout/git/ref/tags/v7.0.1, type=commit)
CHECKOUT_ACTION=actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1

# Go modules and tools (installed later, plans 02-05 and 02-07)
GO_JSONSCHEMA=github.com/santhosh-tekuri/jsonschema/v6@v6.0.3
GO_JWT=github.com/golang-jwt/jwt/v5@v5.3.1
# Approved in Phase 1, reused
SHADCN_CLI=shadcn@4.21.4
```
### 2.2 Verify the pins yourself (about 5 minutes)

You are about to run these binaries on your laptop, so confirm the values rather than trusting this file. Each command prints a value; compare it to `versions.env`.

```bash
. deploy/kind/versions.env

# kind: published asset digest for your OS/arch (needs `gh auth login`)
gh api repos/kubernetes-sigs/kind/releases/tags/${KIND_VERSION} \
  --jq '.assets[] | select(.name | test("kind-(linux|darwin)-(amd64|arm64)$")) | "\(.name) \(.digest)"'

# helm: the publisher's own checksum files
for os_arch in linux-amd64 darwin-arm64 darwin-amd64; do
  echo -n "helm $os_arch "; curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-${os_arch}.tar.gz.sha256sum" | cut -d' ' -f1
done

# images: multi-arch index digests
docker buildx imagetools inspect "${TRAEFIK_IMAGE%@*}" | sed -n '1,4p'
docker buildx imagetools inspect "${WHOAMI_IMAGE%@*}"  | sed -n '1,4p'
docker buildx imagetools inspect "${BUSYBOX_IMAGE%@*}" | sed -n '1,4p'

# Traefik chart digest
curl -fsSL "${TRAEFIK_CHART_REPO}/index.yaml" | grep -A12 "version: ${TRAEFIK_CHART_VERSION}" | grep digest

# Go modules: published, not typosquats (module path, latest tag, origin)
go list -m -versions github.com/golang-jwt/jwt/v5 | tr ' ' '\n' | tail -3
go list -m -versions github.com/santhosh-tekuri/jsonschema/v6 | tr ' ' '\n' | tail -3
```

The two Go modules are the only new runtime dependencies in the Go code:

| Module | Used for | Why this one |
|---|---|---|
| `github.com/golang-jwt/jwt/v5` | App JWT (RS256) in 2a; preview tickets/cookies (HS256) in 2b | the maintained, widely used JWT library; supports `WithValidMethods` to pin the algorithm |
| `github.com/santhosh-tekuri/jsonschema/v6` | validating template values against `values.schema.json` in 2b | pure Go, supports draft-07 (the draft Helm enforces) |

### 2.3 Record the approval

Create `docs/decisions/ADR-0006-phase-2-dependencies.md` (one paragraph is enough): the date, that you verified the commands above, and the list from `versions.env` plus the two modules. The commit that contains it is your "gate passed" marker.

```bash
mkdir -p docs/decisions
cat > docs/decisions/ADR-0006-phase-2-dependencies.md <<'EOF'
# ADR-0006: Phase 2 pinned tools and modules

Status: accepted. Decided by: <your name>, <date>.

I verified the checksums and digests in `deploy/kind/versions.env` against the publishers'
pages (kind release digests, get.helm.sh checksum files, registry index digests, the Traefik
chart index). I approve adding `golang-jwt/jwt/v5` and `santhosh-tekuri/jsonschema/v6` to go.mod.
Everything else in versions.env is a developer tool installed under `.tools/` by `tools/kind.sh`
(Phase 2c), never a Go dependency.
EOF
```

---

## 3. Configuration contract (plan 02-02, task 1)

Every Phase 2 setting becomes a validated field of the one `config.Config` struct, which `api` and `worker` share. Validation fixes four Phase 1 review findings: WR-03 (https without Secure cookies), WR-08 (zero durations), WR-09 (malformed team/repo lists) and the probe-timeout relationship.

### 3.1 `internal/config/config.go` (replace)

Two naming notes, so later code reads naturally: the field `EnvProbeInitial` holds `ENV_PROBE_INTERVAL` (the pause between probe attempts); `PreviewHost` omits the port when it is the default for the scheme.

`internal/config/config.go`

```go
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
	EnvProbeInitial      time.Duration `env:"ENV_PROBE_INTERVAL" envDefault:"5s"`
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
```
### 3.2 `internal/config/validate.go` (replace)

Rules run in four groups, and **positivity runs first**. That ordering matters: with `ENV_POLL_INITIAL=0` the operator should read "ENV_POLL_INITIAL must be > 0", not a confusing "initial must be <= max". Note `PROBE_TIMEOUT` is in the positivity list and the env-probe message names `ENV_PROBE_INTERVAL` (both were defects in an earlier draft of this file).

`internal/config/validate.go`

```go
package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	// orgSlugRe matches org/team-slug and org/repo-name entries (WR-09).
	orgSlugRe = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`)
	// domainRe is a conservative lowercase DNS name; preview hosts are built from it.
	domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
)

// heartbeatMax is half of the 45s heartbeat TTL in internal/queue. A heartbeat
// interval at or above it lets the status page flap to "down" between beats.
const heartbeatMax = 22500 * time.Millisecond

// validate returns the first violated rule, naming the env variable (WR-03,
// WR-08, WR-09). Order matters: positivity first, so a zero duration is
// reported as "must be > 0" and never as a confusing relational error.
func validate(c Config) error {
	// Rule group 1: every duration is strictly positive. PING_THROTTLE is the
	// one exception (0 disables throttling; the e2e overlay relies on it), so
	// it is checked separately for >= 0.
	positive := []struct {
		name  string
		value time.Duration
	}{
		{"SESSION_IDLE_TIMEOUT", c.SessionIdleTimeout},
		{"SESSION_LIFETIME", c.SessionLifetime},
		{"HEARTBEAT_INTERVAL", c.HeartbeatInterval},
		{"PROBE_INTERVAL", c.ProbeInterval},
		{"PROBE_TIMEOUT", c.ProbeTimeout},
		{"PREVIEW_TICKET_TTL", c.PreviewTicketTTL},
		{"PREVIEW_COOKIE_TTL", c.PreviewCookieTTL},
		{"ENV_DEFAULT_TTL", c.EnvDefaultTTL},
		{"ENV_PROVISION_DEADLINE", c.EnvProvisionDeadline},
		{"ENV_POLL_INITIAL", c.EnvPollInitial},
		{"ENV_POLL_MAX", c.EnvPollMax},
		{"ENV_PROBE_INTERVAL", c.EnvProbeInitial},
		{"ENV_PROBE_WINDOW", c.EnvProbeWindow},
		{"ENV_PROBE_TIMEOUT", c.EnvProbeTimeout},
		{"ENV_SWEEP_INTERVAL", c.EnvSweepInterval},
		{"ENV_DISPATCH_SPACING", c.EnvDispatchSpacing},
	}
	for _, p := range positive {
		if p.value <= 0 {
			return fmt.Errorf("%s must be > 0", p.name)
		}
	}
	if c.PingThrottle < 0 {
		return fmt.Errorf("PING_THROTTLE must be >= 0")
	}

	// Rule group 2: relationships between durations.
	switch {
	case c.SessionIdleTimeout > c.SessionLifetime:
		return fmt.Errorf("SESSION_IDLE_TIMEOUT must be <= SESSION_LIFETIME")
	case c.ProbeTimeout >= c.ProbeInterval:
		return fmt.Errorf("PROBE_TIMEOUT must be < PROBE_INTERVAL")
	case c.HeartbeatInterval >= heartbeatMax:
		return fmt.Errorf("HEARTBEAT_INTERVAL must be < %s (half the heartbeat TTL)", heartbeatMax)
	case c.EnvPollInitial > c.EnvPollMax:
		return fmt.Errorf("ENV_POLL_INITIAL must be <= ENV_POLL_MAX")
	case c.EnvProbeTimeout >= c.EnvProbeInitial:
		return fmt.Errorf("ENV_PROBE_TIMEOUT must be < ENV_PROBE_INTERVAL")
	case c.EnvProbeInitial >= c.EnvProbeWindow:
		return fmt.Errorf("ENV_PROBE_INTERVAL must be < ENV_PROBE_WINDOW")
	case c.PreviewTicketTTL > 5*time.Minute:
		return fmt.Errorf("PREVIEW_TICKET_TTL must be <= 5m")
	}

	// Rule group 3: formats.
	lists := []struct {
		name string
		vals []string
	}{
		{"ALLOWED_TEAMS", c.AllowedTeams},
		{"CREATOR_TEAMS", c.CreatorTeams},
		{"ALLOWED_REPOS", c.AllowedRepos},
	}
	for _, l := range lists {
		for _, v := range l.vals {
			if !orgSlugRe.MatchString(v) {
				return fmt.Errorf("%s entry %q must look like org/name", l.name, v)
			}
		}
	}
	if c.EnvironmentsRepo != "" && !orgSlugRe.MatchString(c.EnvironmentsRepo) {
		return fmt.Errorf("ENVIRONMENTS_REPO %q must look like org/name", c.EnvironmentsRepo)
	}
	if !strings.Contains(c.ImageTagFormat, "{sha}") && !strings.Contains(c.ImageTagFormat, "{sha7}") {
		return fmt.Errorf("IMAGE_TAG_FORMAT must contain {sha} or {sha7}")
	}
	if c.PreviewPort < 1 || c.PreviewPort > 65535 {
		return fmt.Errorf("PREVIEW_PORT must be 1..65535")
	}
	if c.PreviewScheme != "http" && c.PreviewScheme != "https" {
		return fmt.Errorf("PREVIEW_SCHEME must be http or https")
	}
	if !domainRe.MatchString(c.PreviewDomain) {
		return fmt.Errorf("PREVIEW_DOMAIN %q is not a valid lowercase domain", c.PreviewDomain)
	}

	// Rule group 4 (WR-03): a Secure-less cookie must never be served over TLS.
	if u, err := url.Parse(c.PublicBaseURL); err == nil && u.Scheme == "https" && !c.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must be true when PUBLIC_BASE_URL is https")
	}
	if c.PreviewScheme == "https" && !c.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must be true when PREVIEW_SCHEME is https")
	}
	return nil
}
```
### 3.3 `internal/config/config_test.go` (replace)

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
```
```bash
bazel test //internal/config/...
```

Expected: `PASSED`. Every rejection test asserts the **error names the variable**, which is what makes a bad deploy debuggable.

### 3.4 `.env.example` (edit)

Append the Phase 2 groups. Copy them from here (the file already has Phase 1 content above):

`.env.example`

```bash
# --- Phase 2: migrations ---
# Optional owner-role DSN used only to run migrations. Falls back to DATABASE_URL when empty.
MIGRATE_DATABASE_URL=

# --- Phase 2: roles and repos ---
# Comma-separated GitHub logins and org/team-slug entries allowed to create environments.
CREATOR_LOGINS=
CREATOR_TEAMS=
# Comma-separated org/name repos that environments may be built from. Empty = nothing allowed (fails closed).
ALLOWED_REPOS=

# --- Phase 2: GitHub App ---
# A GitHub App separate from the OAuth App above. Save the private key under deploy/.secrets/ (git-ignored).
GITHUB_APP_ID=
GITHUB_APP_INSTALLATION_ID=
GITHUB_APP_PRIVATE_KEY_FILE=
# Private repo (org/name) holding the workflow_dispatch provisioning workflow.
ENVIRONMENTS_REPO=
ENVIRONMENTS_WORKFLOW=create-environment.yml
ENVIRONMENTS_REF=main
GHCR_BASE_URL=https://ghcr.io
# The target repo's CI must publish ghcr.io/<org>/<repo>:<full sha> by default. Must contain {sha} or {sha7}.
IMAGE_TAG_FORMAT={sha}

# --- Phase 2: previews ---
# Port 8088 because 8080, 8081, 3000, 3100, 3200, 9090, 9999 and 13133 are already taken.
PREVIEW_DOMAIN=preview.localhost
PREVIEW_PORT=8088
PREVIEW_SCHEME=http
PREVIEW_GATEWAY_ADDR=host.docker.internal:8088
PREVIEW_TOKEN_KEY_FILE=
PREVIEW_TOKEN_PREVIOUS_KEY_FILE=
# One-time ticket lifetime (max 5m) and preview cookie lifetime.
PREVIEW_TICKET_TTL=60s
PREVIEW_COOKIE_TTL=30m

# --- Phase 2: environment lifecycle ---
ENV_DEFAULT_TTL=24h
ENV_PROVISION_DEADLINE=15m
ENV_POLL_INITIAL=3s
ENV_POLL_MAX=15s
ENV_PROBE_INTERVAL=5s
ENV_PROBE_WINDOW=60s
ENV_PROBE_TIMEOUT=2s
ENV_SWEEP_INTERVAL=30s
ENV_DISPATCH_SPACING=1s
```
### 3.5 `.gitignore` (edit)

Append:

```gitignore
# Phase 2 dev secrets and pinned tool binaries
deploy/.secrets/
.tools/
```

### 3.6 `deploy/compose.yaml` (edit: api and worker)

Replace the `api:` and `worker:` services with the blocks below. Three things change: the Phase 2 settings are passed through (all with safe defaults), a read-only secrets mount is added, and **the database connection splits into two roles**: `DATABASE_URL` is now the restricted `ephematool_app` role and `MIGRATE_DATABASE_URL` is the owner, used only by the API at startup to migrate. `extra_hosts` lets the containers reach the host (the kind gateway, later).

`deploy/compose.api-worker.yaml`

```yaml
# Replace the `api:` and `worker:` services in deploy/compose.yaml with these two blocks.
  api:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: api
        VERSION: ${VERSION:-dev}
        COMMIT: ${GIT_COMMIT:-unknown}
        BUILT_AT: ${BUILT_AT:-unknown}
    environment:
      OTEL_SERVICE_NAME: ephematool-api
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
      OTEL_METRIC_EXPORT_INTERVAL: "5000"
      APP_ENV: ${APP_ENV:-local}
      HTTP_ADDR: ${HTTP_ADDR:-:8081}
      # Browser-facing origin: the nginx front door (also the OAuth callback base).
      PUBLIC_BASE_URL: ${PUBLIC_BASE_URL:-http://localhost:8080}
      GRAFANA_PUBLIC_URL: ${GRAFANA_PUBLIC_URL:-http://localhost:3000}
      # Runtime role (D-24): INSERT/SELECT on audit_events, no DDL. DEV-ONLY credentials; the
      # role is created by migration 00002. Production sets its own password out of band.
      DATABASE_URL: ${DATABASE_URL:-postgres://ephematool_app:ephematool_app@postgres:5432/ephematool?sslmode=disable}
      # Owner role used only for migrations (only the api migrates).
      MIGRATE_DATABASE_URL: ${MIGRATE_DATABASE_URL:-postgres://ephematool:ephematool@postgres:5432/ephematool?sslmode=disable}
      MIGRATE_ON_START: ${MIGRATE_ON_START:-true}
      REDIS_ADDR: ${REDIS_ADDR:-redis:6379}
      GITHUB_CLIENT_ID: ${GITHUB_CLIENT_ID:-}
      GITHUB_CLIENT_SECRET: ${GITHUB_CLIENT_SECRET:-}
      GITHUB_AUTHORIZE_URL: ${GITHUB_AUTHORIZE_URL:-https://github.com/login/oauth/authorize}
      GITHUB_TOKEN_URL: ${GITHUB_TOKEN_URL:-https://github.com/login/oauth/access_token}
      GITHUB_API_URL: ${GITHUB_API_URL:-https://api.github.com/}
      ALLOWED_ORGS: ${ALLOWED_ORGS:-}
      ALLOWED_TEAMS: ${ALLOWED_TEAMS:-}
      ADMIN_LOGINS: ${ADMIN_LOGINS:-}
      COOKIE_SECURE: ${COOKIE_SECURE:-false}
      SESSION_IDLE_TIMEOUT: ${SESSION_IDLE_TIMEOUT:-168h}
      SESSION_LIFETIME: ${SESSION_LIFETIME:-720h}
      PING_THROTTLE: ${PING_THROTTLE:-10s}
      HEARTBEAT_INTERVAL: ${HEARTBEAT_INTERVAL:-15s}
      PROBE_INTERVAL: ${PROBE_INTERVAL:-10s}
      PROBE_TIMEOUT: ${PROBE_TIMEOUT:-2s}
      PROMETHEUS_URL: ${PROMETHEUS_URL:-http://prometheus:9090}
      CREATOR_LOGINS: ${CREATOR_LOGINS:-}
      CREATOR_TEAMS: ${CREATOR_TEAMS:-}
      ALLOWED_REPOS: ${ALLOWED_REPOS:-}
      GITHUB_APP_ID: ${GITHUB_APP_ID:-0}
      GITHUB_APP_INSTALLATION_ID: ${GITHUB_APP_INSTALLATION_ID:-0}
      GITHUB_APP_PRIVATE_KEY_FILE: ${GITHUB_APP_PRIVATE_KEY_FILE:-/run/ephematool-secrets/github-app.pem}
      ENVIRONMENTS_REPO: ${ENVIRONMENTS_REPO:-}
      ENVIRONMENTS_WORKFLOW: ${ENVIRONMENTS_WORKFLOW:-create-environment.yml}
      ENVIRONMENTS_REF: ${ENVIRONMENTS_REF:-main}
      GHCR_BASE_URL: ${GHCR_BASE_URL:-https://ghcr.io}
      IMAGE_TAG_FORMAT: ${IMAGE_TAG_FORMAT:-{sha}}
      PREVIEW_DOMAIN: ${PREVIEW_DOMAIN:-preview.localhost}
      PREVIEW_PORT: ${PREVIEW_PORT:-8088}
      PREVIEW_SCHEME: ${PREVIEW_SCHEME:-http}
      PREVIEW_GATEWAY_ADDR: ${PREVIEW_GATEWAY_ADDR:-host.docker.internal:8088}
      PREVIEW_TOKEN_KEY_FILE: ${PREVIEW_TOKEN_KEY_FILE:-/run/ephematool-secrets/preview-token.key}
      PREVIEW_TICKET_TTL: ${PREVIEW_TICKET_TTL:-60s}
      PREVIEW_COOKIE_TTL: ${PREVIEW_COOKIE_TTL:-30m}
      ENV_DEFAULT_TTL: ${ENV_DEFAULT_TTL:-24h}
      ENV_PROVISION_DEADLINE: ${ENV_PROVISION_DEADLINE:-15m}
      ENV_POLL_INITIAL: ${ENV_POLL_INITIAL:-3s}
      ENV_POLL_MAX: ${ENV_POLL_MAX:-15s}
      ENV_PROBE_INTERVAL: ${ENV_PROBE_INTERVAL:-5s}
      ENV_PROBE_WINDOW: ${ENV_PROBE_WINDOW:-60s}
      ENV_PROBE_TIMEOUT: ${ENV_PROBE_TIMEOUT:-2s}
      ENV_SWEEP_INTERVAL: ${ENV_SWEEP_INTERVAL:-30s}
      ENV_DISPATCH_SPACING: ${ENV_DISPATCH_SPACING:-1s}
    volumes:
      - ./.secrets:/run/ephematool-secrets:ro
    extra_hosts:
      - "host.docker.internal:host-gateway"
    ports:
      - "127.0.0.1:8081:8081"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      otel-collector:
        condition: service_started


  worker:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: worker
        VERSION: ${VERSION:-dev}
        COMMIT: ${GIT_COMMIT:-unknown}
        BUILT_AT: ${BUILT_AT:-unknown}
    # The worker binary has no subcommands; ignore the image default CMD.
    command: []
    environment:
      OTEL_SERVICE_NAME: ephematool-worker
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
      OTEL_METRIC_EXPORT_INTERVAL: "5000"
      APP_ENV: ${APP_ENV:-local}
      REDIS_ADDR: ${REDIS_ADDR:-redis:6379}
      # Same restricted app role as the api. The worker never migrates.
      DATABASE_URL: ${DATABASE_URL:-postgres://ephematool_app:ephematool_app@postgres:5432/ephematool?sslmode=disable}
      HEARTBEAT_INTERVAL: ${HEARTBEAT_INTERVAL:-15s}
      GITHUB_API_URL: ${GITHUB_API_URL:-https://api.github.com/}
      CREATOR_LOGINS: ${CREATOR_LOGINS:-}
      CREATOR_TEAMS: ${CREATOR_TEAMS:-}
      ALLOWED_REPOS: ${ALLOWED_REPOS:-}
      GITHUB_APP_ID: ${GITHUB_APP_ID:-0}
      GITHUB_APP_INSTALLATION_ID: ${GITHUB_APP_INSTALLATION_ID:-0}
      GITHUB_APP_PRIVATE_KEY_FILE: ${GITHUB_APP_PRIVATE_KEY_FILE:-/run/ephematool-secrets/github-app.pem}
      ENVIRONMENTS_REPO: ${ENVIRONMENTS_REPO:-}
      ENVIRONMENTS_WORKFLOW: ${ENVIRONMENTS_WORKFLOW:-create-environment.yml}
      ENVIRONMENTS_REF: ${ENVIRONMENTS_REF:-main}
      GHCR_BASE_URL: ${GHCR_BASE_URL:-https://ghcr.io}
      IMAGE_TAG_FORMAT: ${IMAGE_TAG_FORMAT:-{sha}}
      PREVIEW_DOMAIN: ${PREVIEW_DOMAIN:-preview.localhost}
      PREVIEW_PORT: ${PREVIEW_PORT:-8088}
      PREVIEW_SCHEME: ${PREVIEW_SCHEME:-http}
      PREVIEW_GATEWAY_ADDR: ${PREVIEW_GATEWAY_ADDR:-host.docker.internal:8088}
      PREVIEW_TOKEN_KEY_FILE: ${PREVIEW_TOKEN_KEY_FILE:-/run/ephematool-secrets/preview-token.key}
      PREVIEW_TICKET_TTL: ${PREVIEW_TICKET_TTL:-60s}
      PREVIEW_COOKIE_TTL: ${PREVIEW_COOKIE_TTL:-30m}
      ENV_DEFAULT_TTL: ${ENV_DEFAULT_TTL:-24h}
      ENV_PROVISION_DEADLINE: ${ENV_PROVISION_DEADLINE:-15m}
      ENV_POLL_INITIAL: ${ENV_POLL_INITIAL:-3s}
      ENV_POLL_MAX: ${ENV_POLL_MAX:-15s}
      ENV_PROBE_INTERVAL: ${ENV_PROBE_INTERVAL:-5s}
      ENV_PROBE_WINDOW: ${ENV_PROBE_WINDOW:-60s}
      ENV_PROBE_TIMEOUT: ${ENV_PROBE_TIMEOUT:-2s}
      ENV_SWEEP_INTERVAL: ${ENV_SWEEP_INTERVAL:-30s}
      ENV_DISPATCH_SPACING: ${ENV_DISPATCH_SPACING:-1s}
    volumes:
      - ./.secrets:/run/ephematool-secrets:ro
    extra_hosts:
      - "host.docker.internal:host-gateway"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      otel-collector:
        condition: service_started
```
```bash
docker compose -f deploy/compose.yaml config -q && echo OK
docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml config -q && echo OK
```

### 3.7 `tools/stack.sh` (edit: `ensure_secrets`)

Add this function after the `export GIT_COMMIT BUILT_AT` line, and call `ensure_secrets` as the first line of the `up)` and `up-fake)` cases. It creates dev-only keys under the git-ignored `deploy/.secrets/` (mode 700), never overwrites an existing key, and never prints key material.

`tools/stack.sh`

```bash
# Dev-only secrets live in deploy/.secrets (git-ignored). Existing files are never
# overwritten and key material is never printed.
ensure_secrets() {
  local dir=deploy/.secrets
  mkdir -p "$dir"
  chmod 700 "$dir"
  if [ ! -f "$dir/preview-token.key" ]; then
    (umask 077 && openssl rand -hex 32 >"$dir/preview-token.key")
  fi
  if [ ! -f "$dir/github-app-dev.pem" ]; then
    (umask 077 && openssl genrsa -out "$dir/github-app-dev.pem" 2048 2>/dev/null)
  fi
}
```
```bash
bash -n tools/stack.sh && echo syntax-ok
```

---

## 4. One source of truth for label and attribute keys (plan 02-02, task 2)

Phase 1 forbids hand-writing `ephematool.io/…` and `ephematool.env.…` outside `conventions/telemetry.json` (a grep gate enforces it). Phase 2 needs those keys in shell scripts, YAML and Helm, which the Go/TS generators do not reach. The fix is **not** to loosen the gate: the existing generator also writes a shell-sourceable env file, and non-Go consumers read their keys from it.

`tools/genconventions` already supports `-env-out` (it is in Phase 1a). Wire it into Bazel:

### 4.1 `internal/conventions/BUILD.bazel` (edit)

In the `conventions_gen` genrule, add the third output and the flag. The complete file now reads:

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
        "_gen/conventions.gen.env",
    ],
    cmd = "$(execpath //tools/genconventions) -in $(execpath //conventions:telemetry.json) -go-out $(execpath _gen/conventions.gen.go) -ts-out $(execpath _gen/conventions.gen.ts) -env-out $(execpath _gen/conventions.gen.env)",
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
### 4.2 `environments/BUILD.bazel` (new)

`environments/BUILD.bazel`

```python
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")

# conventions.gen.env is generated from conventions/telemetry.json and committed. Shell,
# YAML and Helm consumers read the label/attribute keys from it instead of hand-writing them
# (the D-09 grep gate would reject a hand-written copy). //:check_gen fails when it is stale.
write_source_files(
    name = "gen",
    files = {"conventions.gen.env": "//internal/conventions:_gen/conventions.gen.env"},
    suggested_update_target = "//:gen",
    visibility = ["//:__subpackages__"],
)

exports_files(["conventions.gen.env"])
```
### 4.3 Root `BUILD.bazel` (edit)

Add `"//environments"` to `_GEN_PACKAGES`, so `bazel run //:gen` writes the file and `bazel test //:check_gen` guards against drift:

```python
_GEN_PACKAGES = [
    "//api",
    "//environments",          # new
    "//internal/conventions",
    "//internal/store",
    "//web",
]
```

### 4.4 Generate the file and widen the gate

```bash
bazel run //:gen            # creates environments/conventions.gen.env
cat environments/conventions.gen.env
```

You should see exactly this (sorted; `managed` has no attribute line):

`environments/conventions.gen.env`

```bash
# Code generated by genconventions. DO NOT EDIT.
EPHEMATOOL_LABEL_ENV_ID=ephematool.io/env-id
EPHEMATOOL_LABEL_EXPIRES_AT=ephematool.io/expires-at
EPHEMATOOL_LABEL_MANAGED=ephematool.io/managed
EPHEMATOOL_LABEL_OWNER=ephematool.io/owner
EPHEMATOOL_LABEL_TEMPLATE=ephematool.io/template
EPHEMATOOL_ATTR_ENV_ID=ephematool.env.id
EPHEMATOOL_ATTR_EXPIRES_AT=ephematool.env.expires-at
EPHEMATOOL_ATTR_OWNER=ephematool.env.owner
EPHEMATOOL_ATTR_TEMPLATE=ephematool.env.template
```
Now replace the gate so it scans every tree that can hold deploy code:

`scripts/verify.d/conventions.sh`

```bash
#!/usr/bin/env bash
# D-09 grep gate: the label/attribute prefixes must not be hand-written outside
# conventions/telemetry.json and the generated files. Test files are exempt
# because they assert the literal values.
#
# Phase 2 widened the scan to every tree that can hold deploy or provisioning
# code. Consumers that are not Go/TS source read the keys from the generated
# environments/conventions.gen.env instead: the collector via ${env:...}, kind
# manifests via placeholders that tools/kind.sh substitutes, provision.sh and
# Helm via variables. ".gen." files stay exempt because //:check_gen
# drift-checks them against conventions/telemetry.json.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"
cd "$ROOT"

dirs=()
for d in cmd internal web/src deploy environments templates tools; do [ -d "$d" ] && dirs+=("$d"); done

matches="$(grep -rnF \
  --include='*.go' --include='*.ts' --include='*.tsx' --include='*.yaml' --include='*.yml' \
  --include='*.json' --include='*.sh' --include='*.tpl' --include='*.tmpl' \
  -e 'ephematool.io/' -e 'ephematool.env.' "${dirs[@]}" 2>/dev/null \
  | grep -v -e '\.gen\.' -e 'conventions/telemetry\.json' -e '_test\.go:' -e '_test\.sh:' || true)"

[ -z "$matches" ] || fail "hand-written D-09 prefixes found (use internal/conventions, web/src/gen/conventions.gen.ts or environments/conventions.gen.env):
$matches"
pass "no hand-written D-09 prefixes outside generated files"
```
```bash
bazel test //tools/genconventions/... //:check_gen
bash scripts/verify.d/conventions.sh        # PASS: no hand-written D-09 prefixes ...
bash -c 'set -a; . environments/conventions.gen.env; echo "$EPHEMATOOL_ATTR_ENV_ID"'   # ephematool.env.id
```

> **Check the gate can fail.** Change one value in `conventions/telemetry.json`, run `bazel test //:check_gen` (it must fail with a diff), then revert. A drift check you have never seen fail is not a check.

---

## 5. The fake GitHub, extended (plan 02-03)

The platform talks to GitHub as an App: installation tokens, team membership, branch/PR lookups, a container registry and Actions. None of that can be tested against real GitHub repeatably, so the stdlib-only fake grows to cover it. It stays **test-only** (T-01-26): it ships in tests and in `deploy/compose.e2e.yaml`, never in the base stack.

Design in one paragraph. Branch names decide behaviour: a branch `fail-x` builds a commit whose workflow run **fails**, `stall-x` never finishes, `cancel-x` is cancelled, `no-runid-x` makes the dispatch return 204 (no run id), `noimage-x` has no container image, and `missing-x` does not exist. Commit ids are `sha1("<repo>:<branch>")`, so they are stable across runs. A run is a small state machine (`queued` → `in_progress` → `completed`) advanced by `POST /__fake/runs/{id}/advance` (deterministic, for Go tests) or automatically every `FAKE_RUN_STEP` (for Playwright and demos).

### 5.1 `internal/fakegithub/server.go` (replace)

Changes from Phase 1: two fixtures (`octo-creator` 1007 and `octo-demoted` 1008, both in the creators team), a team override endpoint, the new routes registered in `New()`, mutex-guarded access to the fixture map (it is now mutable), and team membership that accepts installation tokens.

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
	"time"
)

const (
	// ClientID and ClientSecret are the fake OAuth App credentials.
	ClientID     = "e2e-client"
	ClientSecret = "e2e-secret"
	// Org is the fixture organization; Team is the fixture "org/slug" team.
	Org  = "ephematool-e2e"
	Team = "partner-org/sre"
	// CreatorsTeam is the fixture "org/slug" whose members may create environments.
	CreatorsTeam = "ephematool-e2e/creators"
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
		// Phase 2: members of the creators team. octo-demoted is mutated by tests
		// through SetTeamMembership to prove the role is re-checked per request.
		{Login: "octo-creator", ID: 1007, Orgs: map[string]string{Org: "active"}, Teams: map[string]string{CreatorsTeam: "active"}},
		{Login: "octo-demoted", ID: 1008, Orgs: map[string]string{Org: "active"}, Teams: map[string]string{CreatorsTeam: "active"}},
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

	// Phase 2 state, all guarded by mu. Defined in repos.go and actions.go.
	scenarios  map[string]string // known commit sha -> scenario (success, fail, ...)
	instTokens int               // counter for fake-inst-* tokens
	runs       map[int64]*run
	nextRunID  int64
	dispatches []Dispatch
	now        func() time.Time
	stopStep   chan struct{}
}

// New returns a fake GitHub with the canonical fixtures.
func New() *Server {
	s := &Server{
		mux:      http.NewServeMux(),
		fixtures: map[string]Fixture{},
		codes:    map[string]grant{},

		scenarios: map[string]string{},
		runs:      map[int64]*run{},
		nextRunID: 9000000,
		now:       time.Now,
	}
	for _, f := range Fixtures() {
		s.fixtures[f.Login] = f
	}
	s.mux.HandleFunc("GET /login/oauth/authorize", s.authorize)
	s.mux.HandleFunc("POST /login/oauth/access_token", s.accessToken)
	s.mux.HandleFunc("GET /user", s.user)
	s.mux.HandleFunc("GET /user/memberships/orgs/{org}", s.orgMembership)
	s.mux.HandleFunc("GET /orgs/{org}/teams/{slug}/memberships/{login}", s.teamMembership)
	s.mux.HandleFunc("PUT /__fake/teams/{org}/{slug}/memberships/{login}", s.setTeam)
	s.registerApp()
	s.registerRepos()
	s.registerRegistry()
	s.registerActions()
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
		s.mu.Lock()
		_, known := s.fixtures[login]
		s.mu.Unlock()
		if !known {
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

	s.mu.Lock()
	logins := make([]string, 0, len(s.fixtures))
	for l := range s.fixtures {
		logins = append(logins, l)
	}
	s.mu.Unlock()
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
	s.mu.Lock()
	f, ok := s.fixtures[strings.TrimPrefix(tok, "fake-token-")]
	s.mu.Unlock()
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
	if !s.appOrUser(w, r) { // installation tokens (platform) or user tokens (login flow)
		return
	}
	s.mu.Lock()
	f, ok := s.fixtures[r.PathValue("login")]
	state, member := f.Teams[r.PathValue("org")+"/"+r.PathValue("slug")]
	s.mu.Unlock()
	if !ok || !member || state == "none" {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": state, "role": "member"})
}

// SetTeamMembership changes a fixture's membership in "org/slug". state "none"
// removes it (the next GET answers 404); anything else is stored as given.
func (s *Server) SetTeamMembership(org, slug, login, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixtures[login]
	if !ok {
		return
	}
	teams := map[string]string{}
	for k, v := range f.Teams {
		teams[k] = v
	}
	if state == "none" {
		delete(teams, org+"/"+slug)
	} else {
		teams[org+"/"+slug] = state
	}
	f.Teams = teams
	s.fixtures[login] = f
}

func (s *Server) setTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.State == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "state required"})
		return
	}
	s.SetTeamMembership(r.PathValue("org"), r.PathValue("slug"), r.PathValue("login"), body.State)
	w.WriteHeader(http.StatusNoContent)
}
```
### 5.2 `internal/fakegithub/app.go` (new)

The App JWT is validated **structurally only** (three parts, `alg` RS256, `iss`, `exp`, `iat`). No signature check means no private key is ever committed to the repository.

`internal/fakegithub/app.go`

```go
package fakegithub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// registerApp wires the GitHub App installation-token endpoint.
func (s *Server) registerApp() {
	s.mux.HandleFunc("POST /app/installations/{id}/access_tokens", s.accessTokens)
}

// bearer returns the token from an Authorization header ("Bearer x" or "token x").
func bearer(r *http.Request) string {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.TrimPrefix(tok, "token ")
}

// checkAppJWT validates an App JWT *structurally*: three dot-separated parts,
// header alg RS256, payload with a non-empty iss, an exp in the future and an
// iat not more than 60s ahead. The signature is deliberately not checked, so no
// private key ever needs to be committed (T-01-26: test-only fake).
func checkAppJWT(tok string, now time.Time) error {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return fmt.Errorf("want 3 parts, got %d", len(parts))
	}
	dec := func(p string, v any) error {
		raw, err := base64.RawURLEncoding.DecodeString(p)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, v)
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if err := dec(parts[0], &hdr); err != nil || hdr.Alg != "RS256" {
		return fmt.Errorf("alg must be RS256")
	}
	var claims struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
	}
	if err := dec(parts[1], &claims); err != nil || claims.Iss == "" {
		return fmt.Errorf("iss required")
	}
	if claims.Exp <= now.Unix() {
		return fmt.Errorf("expired")
	}
	if claims.Iat > now.Add(60*time.Second).Unix() {
		return fmt.Errorf("iat in the future")
	}
	return nil
}

func (s *Server) accessTokens(w http.ResponseWriter, r *http.Request) {
	if err := checkAppJWT(bearer(r), s.now()); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	s.mu.Lock()
	s.instTokens++
	n := s.instTokens
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{
		"token":      fmt.Sprintf("fake-inst-%s-%d", r.PathValue("id"), n),
		"expires_at": s.now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
}

// InstallationTokens reports how many installation tokens were minted. Tests
// use it to prove the client caches its token.
func (s *Server) InstallationTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instTokens
}

// appOrUser accepts a fake installation token or a fake user token. It writes
// the 401 itself and reports whether the caller may continue.
func (s *Server) appOrUser(w http.ResponseWriter, r *http.Request) bool {
	tok := bearer(r)
	if strings.HasPrefix(tok, "fake-inst-") {
		return true
	}
	_, ok := s.caller(w, r)
	return ok
}
```
### 5.3 `internal/fakegithub/repos.go` (new)

`internal/fakegithub/repos.go`

```go
package fakegithub

import (
	"crypto/sha1" //nolint:gosec // not security: deterministic fake commit ids
	"encoding/hex"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Repos is the fixed set of repositories the fake knows (all lowercase).
var Repos = []string{
	"ephematool-e2e/demo-app",
	"ephematool-e2e/other-app",
	"ephematool-e2e/ephematool-environments",
}

var (
	branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	hexRe    = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// scenarioPrefixes are branch-name prefixes that select how a build behaves.
// The scenario is remembered per resolved commit sha.
var scenarioPrefixes = map[string]string{
	"fail-": "fail", "stall-": "stall", "cancel-": "cancel",
	"no-runid-": "no-runid", "noimage-": "noimage",
}

func (s *Server) repoKnown(repo string) bool {
	for _, r := range Repos {
		if strings.EqualFold(r, repo) {
			return true
		}
	}
	return false
}

// shaFor derives the stable fake commit id for a branch in a repo.
func shaFor(repo, branch string) string {
	sum := sha1.Sum([]byte(strings.ToLower(repo) + ":" + branch)) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

// remember records the scenario for a sha (default "success") and returns sha.
func (s *Server) remember(sha, branch string) string {
	scn := "success"
	for prefix, name := range scenarioPrefixes {
		if strings.HasPrefix(branch, prefix) {
			scn = name
		}
	}
	s.mu.Lock()
	s.scenarios[sha] = scn
	s.mu.Unlock()
	return sha
}

func (s *Server) scenario(sha string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scn, ok := s.scenarios[sha]; ok {
		return scn
	}
	return "success"
}

func (s *Server) registerRepos() {
	// The three fixed branches exist for every known repo from startup.
	for _, repo := range Repos {
		for _, b := range []string{"main", "feature/x", "release/1.2"} {
			s.remember(shaFor(repo, b), b)
		}
	}
	s.mux.HandleFunc("GET /repos/{o}/{r}/git/ref/heads/{b...}", s.getRef)
	s.mux.HandleFunc("GET /repos/{o}/{r}/commits/{ref}", s.getCommit)
	s.mux.HandleFunc("GET /repos/{o}/{r}/pulls/{n}", s.getPull)
}

func (s *Server) getRef(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	repo := r.PathValue("o") + "/" + r.PathValue("r")
	branch := r.PathValue("b")
	if !s.repoKnown(repo) || !branchRe.MatchString(branch) || strings.HasPrefix(branch, "missing-") {
		notFound(w)
		return
	}
	sha := s.remember(shaFor(repo, branch), branch)
	writeJSON(w, http.StatusOK, map[string]any{
		"ref":    "refs/heads/" + branch,
		"object": map[string]string{"type": "commit", "sha": sha},
	})
}

func (s *Server) getCommit(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	ref := strings.ToLower(r.PathValue("ref"))
	if !hexRe.MatchString(ref) {
		notFound(w)
		return
	}
	s.mu.Lock()
	var found []string
	for sha := range s.scenarios {
		if strings.HasPrefix(sha, ref) {
			found = append(found, sha)
		}
	}
	s.mu.Unlock()
	if len(found) != 1 { // unknown, or an ambiguous prefix
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sha": found[0]})
}

// getPull serves four fixture pull requests: 1 same-repo open, 2 from a fork,
// 3 from a deleted fork (head.repo null), 4 closed. Anything else is 404.
func (s *Server) getPull(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	repo := r.PathValue("o") + "/" + r.PathValue("r")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || !s.repoKnown(repo) || n < 1 || n > 4 {
		notFound(w)
		return
	}
	headRepo := any(map[string]string{"full_name": repo})
	headSHA := s.remember(shaFor(repo, "feature/x"), "feature/x")
	state := "open"
	switch n {
	case 2:
		fork := "octo-fork/demo-app"
		headRepo = map[string]string{"full_name": fork}
		headSHA = s.remember(shaFor(fork, "feature/x"), "feature/x")
	case 3:
		headRepo = nil
	case 4:
		state = "closed"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"number": n,
		"state":  state,
		"head":   map[string]any{"ref": "feature/x", "sha": headSHA, "repo": headRepo},
		"base":   map[string]any{"ref": "main", "repo": map[string]string{"full_name": repo}},
	})
}
```
### 5.4 `internal/fakegithub/registry.go` (new)

`internal/fakegithub/registry.go`

```go
package fakegithub

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

const registryToken = "fake-registry"

// registerRegistry wires a minimal GHCR: token endpoint plus manifest GET/HEAD.
func (s *Server) registerRegistry() {
	s.mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"token": registryToken})
	})
	// A "GET" pattern also serves HEAD; net/http drops the body for HEAD.
	s.mux.HandleFunc("GET /v2/{org}/{repo}/manifests/{tag}", s.manifest)
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	if bearer(r) != registryToken {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="ghcr.io"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []map[string]string{{"code": "UNAUTHORIZED"}}})
		return
	}
	if !s.repoKnown(r.PathValue("org") + "/" + r.PathValue("repo")) {
		writeJSON(w, http.StatusForbidden, map[string]any{"errors": []map[string]string{{"code": "DENIED"}}})
		return
	}
	tag := strings.ToLower(r.PathValue("tag"))
	if !hexRe.MatchString(tag) || !s.tagHasImage(tag) {
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []map[string]string{{"code": "MANIFEST_UNKNOWN"}}})
		return
	}
	sum := sha256.Sum256([]byte(tag))
	w.Header().Set("Docker-Content-Digest", "sha256:"+hex.EncodeToString(sum[:]))
	w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"schemaVersion":2}`))
}

// tagHasImage reports whether tag is a known sha (or a prefix of exactly one)
// whose scenario is not "noimage".
func (s *Server) tagHasImage(tag string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var match string
	n := 0
	for sha := range s.scenarios {
		if strings.HasPrefix(sha, tag) {
			match, n = sha, n+1
		}
	}
	return n == 1 && s.scenarios[match] != "noimage"
}
```
### 5.5 `internal/fakegithub/actions.go` (new)

The dispatch handler reproduces both GitHub API shapes: `200` with run details when `return_run_details` is true (or API version `2026-03-10`), `204` otherwise. Input limits (25 inputs, 65,535 characters) are enforced because the real API enforces them.

`internal/fakegithub/actions.go`

```go
package fakegithub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dispatch is one recorded workflow_dispatch call.
type Dispatch struct {
	File   string
	Ref    string
	Inputs map[string]string
	At     time.Time
}

// run is one fake workflow run: queued -> in_progress -> completed.
type run struct {
	id         int64
	repo       string
	title      string
	inputs     map[string]string
	scenario   string
	status     string // queued | in_progress | completed
	conclusion string // "" until completed
	version    int
	created    time.Time
	started    time.Time
	updated    time.Time
}

const (
	maxInputs     = 25
	maxInputChars = 65535
)

func (s *Server) registerActions() {
	s.mux.HandleFunc("POST /repos/{o}/{r}/actions/workflows/{file}/dispatches", s.dispatch)
	s.mux.HandleFunc("GET /repos/{o}/{r}/actions/runs", s.listRuns)
	s.mux.HandleFunc("GET /repos/{o}/{r}/actions/runs/{id}", s.getRun)
	s.mux.HandleFunc("GET /repos/{o}/{r}/actions/runs/{id}/jobs", s.runJobs)
	s.mux.HandleFunc("POST /__fake/runs/{id}/advance", s.advanceHTTP)
	s.mux.HandleFunc("GET /__fake/dispatches", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Dispatches())
	})
	// Stand-in for the preview gateway that the worker probes in the e2e stack.
	s.mux.HandleFunc("GET /__ephematool/healthz", s.previewHealth)
}

func (s *Server) htmlURL(r *http.Request, repo string, id int64) string {
	return fmt.Sprintf("http://%s/%s/actions/runs/%d", r.Host, repo, id)
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	repo := r.PathValue("o") + "/" + r.PathValue("r")
	if !s.repoKnown(repo) {
		notFound(w)
		return
	}
	var body struct {
		Ref              string         `json:"ref"`
		Inputs           map[string]any `json:"inputs"`
		ReturnRunDetails bool           `json:"return_run_details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "ref required"})
		return
	}
	inputs := map[string]string{}
	total := 0
	for k, v := range body.Inputs {
		sv := fmt.Sprint(v)
		inputs[k] = sv
		total += len(sv)
	}
	if len(inputs) > maxInputs || total > maxInputChars {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"message": "Invalid Request. Too many inputs or inputs too large"})
		return
	}
	scn := s.scenario(inputs["sha"])

	s.mu.Lock()
	s.nextRunID++
	now := s.now()
	ru := &run{
		id: s.nextRunID, repo: repo, inputs: inputs, scenario: scn,
		title:  "create " + inputs["env_id"] + " " + inputs["correlation_id"],
		status: "queued", version: 1, created: now, updated: now,
	}
	s.runs[ru.id] = ru
	s.dispatches = append(s.dispatches, Dispatch{File: r.PathValue("file"), Ref: body.Ref, Inputs: inputs, At: now})
	id := ru.id
	s.mu.Unlock()

	// GitHub API 2022-11-28 answers 200 only when return_run_details is set;
	// 2026-03-10 always answers 200. The no-runid scenario always answers 204
	// so the client's correlation-search fallback can be exercised.
	wantsDetails := body.ReturnRunDetails || r.Header.Get("X-GitHub-Api-Version") == "2026-03-10"
	if scn == "no-runid" || !wantsDetails {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflow_run_id": id,
		"run_url":         fmt.Sprintf("http://%s/repos/%s/actions/runs/%d", r.Host, repo, id),
		"html_url":        s.htmlURL(r, repo, id),
	})
}

// runJSON renders a run. Caller holds s.mu.
func (s *Server) runJSON(r *http.Request, ru *run) map[string]any {
	m := map[string]any{
		"id": ru.id, "name": "create-environment", "display_title": ru.title,
		"status": ru.status, "event": "workflow_dispatch",
		"html_url":   s.htmlURL(r, ru.repo, ru.id),
		"created_at": ru.created.UTC().Format(time.RFC3339),
		"updated_at": ru.updated.UTC().Format(time.RFC3339),
	}
	m["conclusion"] = nil
	if ru.conclusion != "" {
		m["conclusion"] = ru.conclusion
	}
	if !ru.started.IsZero() {
		m["run_started_at"] = ru.started.UTC().Format(time.RFC3339)
	}
	return m
}

func etag(ru *run) string { return fmt.Sprintf(`W/"%d-%d"`, ru.id, ru.version) }

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	q := r.URL.Query()
	var since time.Time
	if c := q.Get("created"); c != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimPrefix(c, ">="))
		if err == nil {
			since = t
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*run
	for _, ru := range s.runs {
		if ru.repo != r.PathValue("o")+"/"+r.PathValue("r") {
			continue
		}
		if ev := q.Get("event"); ev != "" && ev != "workflow_dispatch" {
			continue
		}
		// Compare at second resolution like GitHub does.
		if !since.IsZero() && ru.created.Truncate(time.Second).Before(since) {
			continue
		}
		out = append(out, ru)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id > out[j].id })
	runs := make([]map[string]any, 0, len(out))
	for _, ru := range out {
		runs = append(runs, s.runJSON(r, ru))
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(runs), "workflow_runs": runs})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*run, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	ru := s.runs[id]
	s.mu.Unlock()
	if err != nil || ru == nil || ru.repo != r.PathValue("o")+"/"+r.PathValue("r") {
		notFound(w)
		return nil, false
	}
	return ru, true
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	ru, ok := s.lookup(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tag := etag(ru)
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, s.runJSON(r, ru))
}

func (s *Server) runJobs(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	ru, ok := s.lookup(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339)
	}
	base := ru.started
	steps := make([]map[string]any, 0, 3)
	for i, name := range []string{"Validate inputs", "Provision namespace", "Helm install"} {
		st := map[string]any{"name": name, "number": i + 1, "status": ru.status, "conclusion": nil,
			"started_at": nil, "completed_at": nil}
		if !base.IsZero() {
			st["started_at"] = ts(base.Add(time.Duration(i) * time.Second))
		}
		if ru.status == "completed" {
			st["conclusion"] = ru.conclusion
			st["completed_at"] = ts(base.Add(time.Duration(i+1) * time.Second))
		}
		steps = append(steps, st)
	}
	job := map[string]any{"name": "provision", "status": ru.status, "conclusion": nil,
		"started_at": ts(base), "completed_at": nil, "steps": steps}
	if ru.status == "completed" {
		job["conclusion"] = ru.conclusion
		job["completed_at"] = ts(base.Add(3 * time.Second))
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": 1, "jobs": []any{job}})
}

// advance performs one lifecycle transition. Caller holds s.mu.
func (s *Server) advance(ru *run) {
	switch ru.status {
	case "queued":
		ru.status = "in_progress"
		ru.started = s.now()
	case "in_progress":
		if ru.scenario == "stall" {
			return // stays in progress forever
		}
		ru.status = "completed"
		ru.conclusion = map[string]string{"fail": "failure", "cancel": "cancelled"}[ru.scenario]
		if ru.conclusion == "" {
			ru.conclusion = "success"
		}
	default:
		return
	}
	ru.version++
	ru.updated = s.now()
}

// AdvanceRun performs one transition on a run and reports whether it exists.
func (s *Server) AdvanceRun(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ru, ok := s.runs[id]
	if ok {
		s.advance(ru)
	}
	return ok
}

func (s *Server) advanceHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || !s.AdvanceRun(id) {
		notFound(w)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.runJSON(r, s.runs[id]))
}

// SetRunStep auto-advances every non-terminal run each d. d == 0 stops it
// (manual mode). Safe to call repeatedly.
func (s *Server) SetRunStep(d time.Duration) {
	s.mu.Lock()
	if s.stopStep != nil {
		close(s.stopStep)
		s.stopStep = nil
	}
	if d <= 0 {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.stopStep = stop
	s.mu.Unlock()
	go func() {
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.mu.Lock()
				for _, ru := range s.runs {
					s.advance(ru)
				}
				s.mu.Unlock()
			}
		}
	}()
}

// Dispatches returns a copy of every recorded workflow_dispatch call.
func (s *Server) Dispatches() []Dispatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Dispatch(nil), s.dispatches...)
}

// previewHealth stands in for the preview gateway: 200 only after the run for
// the env named by the Host's first label completed successfully.
func (s *Server) previewHealth(w http.ResponseWriter, r *http.Request) {
	envID, _, _ := strings.Cut(r.Host, ".")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ru := range s.runs {
		if ru.inputs["env_id"] == envID && ru.conclusion == "success" {
			_, _ = w.Write([]byte("ok"))
			return
		}
	}
	http.Error(w, "not ready", http.StatusServiceUnavailable)
}
```
### 5.6 `cmd/fakegithub/main.go` (replace)

`cmd/fakegithub/main.go`

```go
// Command fakegithub runs the fake GitHub for dev/e2e login and previews. Test-only.
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
	// FAKE_RUN_STEP > 0 makes dispatched runs advance by themselves every step
	// (handy for Playwright and demos); 0 means tests advance runs manually.
	step := time.Duration(0)
	if v := os.Getenv("FAKE_RUN_STEP"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fatal: FAKE_RUN_STEP:", err)
			os.Exit(2)
		}
		step = d
	}
	fake := fakegithub.New()
	fake.SetRunStep(step)
	fmt.Println("fakegithub listening on", addr, "run step", step)
	srv := &http.Server{Addr: addr, Handler: fake, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
```
### 5.7 Tests

`server_test.go` (Phase 1) is unchanged. The two new files cover the new behaviour; they build a structurally valid JWT by hand so the package stays stdlib-only.

`internal/fakegithub/repos_test.go`

```go
package fakegithub

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// appJWT builds a structurally valid JWT by hand (base64url JSON), so this
// package stays stdlib-only. alg and the claims are what the fake validates.
func appJWT(t *testing.T, alg string, iss string, exp time.Time, parts int) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	hdr := enc(map[string]string{"alg": alg, "typ": "JWT"})
	claims := enc(map[string]any{"iss": iss, "iat": time.Now().Add(-60 * time.Second).Unix(), "exp": exp.Unix()})
	if parts == 2 {
		return hdr + "." + claims
	}
	// The signature is not checked by the fake, so a placeholder is enough.
	return hdr + "." + claims + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
}

func do(t *testing.T, method, url, token string, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, b.String()
}

func instToken(t *testing.T, base string) string {
	t.Helper()
	jwt := appJWT(t, "RS256", "12345", time.Now().Add(9*time.Minute), 3)
	resp, body := do(t, "POST", base+"/app/installations/777/access_tokens", jwt, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("access_tokens: %d %s", resp.StatusCode, body)
	}
	var out struct{ Token string }
	_ = json.Unmarshal([]byte(body), &out)
	return out.Token
}

func TestAccessTokens(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	if tok := instToken(t, ts.URL); !strings.HasPrefix(tok, "fake-inst-") {
		t.Fatalf("token %q lacks fake-inst- prefix", tok)
	}
	bad := map[string]string{
		"alg none":  appJWT(t, "none", "1", time.Now().Add(time.Minute), 3),
		"expired":   appJWT(t, "RS256", "1", time.Now().Add(-time.Minute), 3),
		"two parts": appJWT(t, "RS256", "1", time.Now().Add(time.Minute), 2),
		"empty iss": appJWT(t, "RS256", "", time.Now().Add(time.Minute), 3),
		"no bearer": "",
		"not a jwt": "garbage",
	}
	for name, jwt := range bad {
		if resp, _ := do(t, "POST", ts.URL+"/app/installations/777/access_tokens", jwt, ""); resp.StatusCode != 401 {
			t.Errorf("%s: got %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestTeamControl(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	path := "/orgs/ephematool-e2e/teams/creators/memberships/"
	if resp, body := do(t, "GET", ts.URL+path+"octo-creator", inst, ""); resp.StatusCode != 200 || !strings.Contains(body, "active") {
		t.Fatalf("octo-creator: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, "GET", ts.URL+path+"octo-viewer", inst, ""); resp.StatusCode != 404 {
		t.Fatalf("octo-viewer: got %d, want 404", resp.StatusCode)
	}
	resp, _ := do(t, "PUT", ts.URL+"/__fake/teams/ephematool-e2e/creators/memberships/octo-demoted", "", `{"state":"none"}`)
	if resp.StatusCode != 204 {
		t.Fatalf("PUT team: %d", resp.StatusCode)
	}
	if resp, _ := do(t, "GET", ts.URL+path+"octo-demoted", inst, ""); resp.StatusCode != 404 {
		t.Fatalf("octo-demoted after removal: got %d, want 404", resp.StatusCode)
	}
	if resp, _ := do(t, "GET", ts.URL+path+"octo-creator", "", ""); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated: got %d, want 401", resp.StatusCode)
	}
}

func refSHA(t *testing.T, base, tok, repo, branch string) (int, string) {
	t.Helper()
	resp, body := do(t, "GET", base+"/repos/"+repo+"/git/ref/heads/"+branch, tok, "")
	var out struct {
		Object struct{ SHA string }
	}
	_ = json.Unmarshal([]byte(body), &out)
	return resp.StatusCode, out.Object.SHA
}

func TestRefsAndCommits(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	repo := "ephematool-e2e/demo-app"

	st, mainSHA := refSHA(t, ts.URL, inst, repo, "main")
	if st != 200 || len(mainSHA) != 40 {
		t.Fatalf("main: %d %q", st, mainSHA)
	}
	if _, again := refSHA(t, ts.URL, inst, repo, "main"); again != mainSHA {
		t.Fatal("sha not stable")
	}
	if st, sha := refSHA(t, ts.URL, inst, repo, "feature/x"); st != 200 || sha == mainSHA {
		t.Fatalf("feature/x: %d %q", st, sha)
	}
	if st, _ := refSHA(t, ts.URL, inst, repo, "missing-x"); st != 404 {
		t.Fatalf("missing-x: %d", st)
	}
	if st, _ := refSHA(t, ts.URL, "", repo, "main"); st != 401 {
		t.Fatalf("unauthenticated: %d", st)
	}
	if st, _ := refSHA(t, ts.URL, inst, "nope/none", "main"); st != 404 {
		t.Fatalf("unknown repo: %d", st)
	}

	resp, body := do(t, "GET", ts.URL+"/repos/"+repo+"/commits/"+mainSHA[:7], inst, "")
	if resp.StatusCode != 200 || !strings.Contains(body, mainSHA) {
		t.Fatalf("short sha: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, "GET", ts.URL+"/repos/"+repo+"/commits/deadbee", inst, ""); resp.StatusCode != 404 {
		t.Fatalf("unknown sha: %d", resp.StatusCode)
	}
}

func TestPulls(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	_, featureSHA := refSHA(t, ts.URL, inst, "ephematool-e2e/demo-app", "feature/x")
	get := func(n string) (int, map[string]any) {
		resp, body := do(t, "GET", ts.URL+"/repos/ephematool-e2e/demo-app/pulls/"+n, inst, "")
		var m map[string]any
		_ = json.Unmarshal([]byte(body), &m)
		return resp.StatusCode, m
	}
	_, p1 := get("1")
	head := p1["head"].(map[string]any)
	if head["sha"] != featureSHA || head["repo"].(map[string]any)["full_name"] != "ephematool-e2e/demo-app" {
		t.Errorf("pr1 head = %v", head)
	}
	_, p2 := get("2")
	if p2["head"].(map[string]any)["repo"].(map[string]any)["full_name"] != "octo-fork/demo-app" {
		t.Errorf("pr2 must come from the fork: %v", p2["head"])
	}
	_, p3 := get("3")
	if p3["head"].(map[string]any)["repo"] != nil {
		t.Errorf("pr3 head.repo must be null: %v", p3["head"])
	}
	_, p4 := get("4")
	if p4["state"] != "closed" {
		t.Errorf("pr4 state = %v", p4["state"])
	}
	if st, _ := get("99"); st != 404 {
		t.Errorf("pr99: %d", st)
	}
}

func TestRegistry(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	_, mainSHA := refSHA(t, ts.URL, inst, "ephematool-e2e/demo-app", "main")
	_, noimage := refSHA(t, ts.URL, inst, "ephematool-e2e/demo-app", "noimage-x")
	manifest := func(method, repo, tag, tok string) (*http.Response, string) {
		return do(t, method, ts.URL+"/v2/"+repo+"/manifests/"+tag, tok, "")
	}

	resp, _ := manifest("GET", "ephematool-e2e/demo-app", mainSHA, "")
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer realm=") {
		t.Fatalf("no bearer: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	_, tokBody := do(t, "GET", ts.URL+"/token?scope=repository:ephematool-e2e/demo-app:pull", "", "")
	var tok struct{ Token string }
	_ = json.Unmarshal([]byte(tokBody), &tok)

	get, _ := manifest("GET", "ephematool-e2e/demo-app", mainSHA, tok.Token)
	if get.StatusCode != 200 || !strings.HasPrefix(get.Header.Get("Docker-Content-Digest"), "sha256:") {
		t.Fatalf("manifest: %d %v", get.StatusCode, get.Header)
	}
	head, hb := manifest("HEAD", "ephematool-e2e/demo-app", mainSHA, tok.Token)
	if head.StatusCode != 200 || hb != "" || head.Header.Get("Docker-Content-Digest") != get.Header.Get("Docker-Content-Digest") {
		t.Fatalf("HEAD must mirror GET with an empty body: %d %q", head.StatusCode, hb)
	}
	if r, _ := manifest("GET", "ephematool-e2e/demo-app", noimage, tok.Token); r.StatusCode != 404 {
		t.Fatalf("noimage: %d", r.StatusCode)
	}
	if r, _ := manifest("GET", "somebody/else", mainSHA, tok.Token); r.StatusCode != 403 {
		t.Fatalf("unknown repo: %d", r.StatusCode)
	}
	if r, _ := manifest("GET", "ephematool-e2e/demo-app", mainSHA[:7], tok.Token); r.StatusCode != 200 {
		t.Fatalf("sha7 tag: %d", r.StatusCode)
	}
}
```
`internal/fakegithub/actions_test.go`

```go
package fakegithub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const envsRepo = "ephematool-e2e/ephematool-environments"

type rig struct {
	t    *testing.T
	srv  *Server
	ts   *httptest.Server
	inst string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	srv := New()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { srv.SetRunStep(0) })
	return &rig{t: t, srv: srv, ts: ts, inst: instToken(t, ts.URL)}
}

func (g *rig) sha(branch string) string {
	_, sha := refSHA(g.t, g.ts.URL, g.inst, "ephematool-e2e/demo-app", branch)
	return sha
}

// dispatch posts a workflow dispatch for envID built from branch.
func (g *rig) dispatch(envID, branch string, extra map[string]any, headers map[string]string) (*http.Response, string) {
	g.t.Helper()
	inputs := map[string]any{"env_id": envID, "correlation_id": "corr-" + envID, "traceparent": "00-aa-bb-01", "sha": g.sha(branch)}
	for k, v := range extra {
		inputs[k] = v
	}
	body, _ := json.Marshal(map[string]any{"ref": "main", "inputs": inputs, "return_run_details": extra["_details"] != false})
	req, _ := http.NewRequest("POST", g.ts.URL+"/repos/"+envsRepo+"/actions/workflows/create-environment.yml/dispatches", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+g.inst)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer resp.Body.Close()
	b := new(strings.Builder)
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	b.Write(buf[:n])
	return resp, b.String()
}

func runID(t *testing.T, body string) int64 {
	t.Helper()
	var out struct {
		ID int64 `json:"workflow_run_id"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.ID == 0 {
		t.Fatalf("no workflow_run_id in %q", body)
	}
	return out.ID
}

func (g *rig) getRun(id int64, ifNoneMatch string) (*http.Response, map[string]any) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/actions/runs/%d", g.ts.URL, envsRepo, id), nil)
	req.Header.Set("Authorization", "Bearer "+g.inst)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func (g *rig) advance(id int64) {
	g.t.Helper()
	resp, _ := do(g.t, "POST", fmt.Sprintf("%s/__fake/runs/%d/advance", g.ts.URL, id), "", "")
	if resp.StatusCode != 200 {
		g.t.Fatalf("advance: %d", resp.StatusCode)
	}
}

func TestDispatchStatusCodes(t *testing.T) {
	g := newRig(t)
	if resp, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil); resp.StatusCode != 200 || !strings.Contains(body, "html_url") {
		t.Fatalf("with return_run_details: %d %s", resp.StatusCode, body)
	}
	if resp, _ := g.dispatch("pv-bbbbbbbbbb", "main", map[string]any{"_details": false}, nil); resp.StatusCode != 204 {
		t.Fatalf("without flag: got %d, want 204", resp.StatusCode)
	}
	if resp, _ := g.dispatch("pv-cccccccccc", "main", map[string]any{"_details": false}, map[string]string{"X-GitHub-Api-Version": "2026-03-10"}); resp.StatusCode != 200 {
		t.Fatalf("api 2026-03-10: got %d, want 200", resp.StatusCode)
	}
	if resp, _ := g.dispatch("pv-dddddddddd", "no-runid-x", nil, nil); resp.StatusCode != 204 {
		t.Fatalf("no-runid: got %d, want 204", resp.StatusCode)
	}
	many := map[string]any{}
	for i := 0; i < 22; i++ { // 4 base inputs + 22 = 26 > 25
		many[fmt.Sprintf("k%d", i)] = "v"
	}
	if resp, _ := g.dispatch("pv-eeeeeeeeee", "main", many, nil); resp.StatusCode != 422 {
		t.Fatalf("26 inputs: got %d, want 422", resp.StatusCode)
	}
	if got := len(g.srv.Dispatches()); got != 4 {
		t.Fatalf("recorded %d dispatches, want 4 (the 422 is not recorded)", got)
	}
}

func TestDispatchRecordsInputsAndListsRun(t *testing.T) {
	g := newRig(t)
	before := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	d := g.srv.Dispatches()
	if len(d) != 1 || d[0].Inputs["env_id"] != "pv-aaaaaaaaaa" || d[0].Inputs["traceparent"] != "00-aa-bb-01" {
		t.Fatalf("dispatch not recorded verbatim: %+v", d)
	}
	resp, body := do(t, "GET", g.ts.URL+"/repos/"+envsRepo+"/actions/runs?event=workflow_dispatch&created=>="+before, g.inst, "")
	if resp.StatusCode != 200 || !strings.Contains(body, "create pv-aaaaaaaaaa corr-pv-aaaaaaaaaa") {
		t.Fatalf("run list: %d %s", resp.StatusCode, body)
	}
}

func TestRunLifecycleAndETag(t *testing.T) {
	g := newRig(t)
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)

	resp, run := g.getRun(id, "")
	tag := resp.Header.Get("ETag")
	if tag == "" || run["status"] != "queued" {
		t.Fatalf("queued run: %q %v", tag, run["status"])
	}
	if resp, _ := g.getRun(id, tag); resp.StatusCode != 304 {
		t.Fatalf("If-None-Match: got %d, want 304", resp.StatusCode)
	}
	g.advance(id)
	resp, run = g.getRun(id, tag)
	if resp.StatusCode != 200 || run["status"] != "in_progress" || resp.Header.Get("ETag") == tag {
		t.Fatalf("after advance: %d %v etag %q", resp.StatusCode, run["status"], resp.Header.Get("ETag"))
	}
	g.advance(id)
	if _, run = g.getRun(id, ""); run["status"] != "completed" || run["conclusion"] != "success" {
		t.Fatalf("completed: %v %v", run["status"], run["conclusion"])
	}
}

func TestScenarios(t *testing.T) {
	g := newRig(t)
	cases := []struct{ branch, status, conclusion string }{
		{"fail-x", "completed", "failure"},
		{"cancel-x", "completed", "cancelled"},
		{"stall-x", "in_progress", ""},
	}
	for i, c := range cases {
		_, body := g.dispatch(fmt.Sprintf("pv-scenario%02d", i), c.branch, nil, nil)
		id := runID(t, body)
		for j := 0; j < 5; j++ {
			g.advance(id)
		}
		_, run := g.getRun(id, "")
		got, _ := run["conclusion"].(string)
		if run["status"] != c.status || got != c.conclusion {
			t.Errorf("%s: status %v conclusion %q, want %s %q", c.branch, run["status"], got, c.status, c.conclusion)
		}
	}
}

func TestJobs(t *testing.T) {
	g := newRig(t)
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)
	g.advance(id)
	g.advance(id)
	_, jb := do(t, "GET", fmt.Sprintf("%s/repos/%s/actions/runs/%d/jobs", g.ts.URL, envsRepo, id), g.inst, "")
	var out struct {
		Jobs []struct {
			Name        string
			Conclusion  string
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
			Steps       []struct {
				Name        string
				Number      int
				StartedAt   time.Time `json:"started_at"`
				CompletedAt time.Time `json:"completed_at"`
			}
		}
	}
	if err := json.Unmarshal([]byte(jb), &out); err != nil || len(out.Jobs) != 1 {
		t.Fatalf("jobs: %v %s", err, jb)
	}
	j := out.Jobs[0]
	if j.Name != "provision" || j.Conclusion != "success" || len(j.Steps) != 3 {
		t.Fatalf("job = %+v", j)
	}
	for i, st := range j.Steps {
		if st.Number != i+1 || !st.CompletedAt.After(st.StartedAt) {
			t.Errorf("step %d timestamps out of order: %+v", i, st)
		}
		if i > 0 && st.StartedAt.Before(j.Steps[i-1].StartedAt) {
			t.Errorf("step %d starts before step %d", i, i-1)
		}
	}
}

func TestPreviewHealth(t *testing.T) {
	g := newRig(t)
	probe := func() int {
		req, _ := http.NewRequest("GET", g.ts.URL+"/__ephematool/healthz", nil)
		req.Host = "pv-aaaaaaaaaa.preview.localhost:8088"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if probe() != 503 {
		t.Fatal("before any run the probe must be 503")
	}
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)
	g.advance(id)
	if probe() != 503 {
		t.Fatal("while in progress the probe must be 503")
	}
	g.advance(id)
	if probe() != 200 {
		t.Fatal("after success the probe must be 200")
	}
}

func TestAutoAdvance(t *testing.T) {
	g := newRig(t)
	g.srv.SetRunStep(20 * time.Millisecond)
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, run := g.getRun(id, ""); run["conclusion"] == "success" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not complete within 1s of auto-advance")
}
```
```bash
bazel run //:gazelle
bazel test //internal/fakegithub/... && bazel build //cmd/fakegithub
```

---

## 6. Schema, grants and the audit log (plan 02-04)

Two roles, on purpose (D-24). The **owner** (`ephematool`) creates tables and runs migrations. The runtime role **`ephematool_app`** can read and write application tables, but on `audit_events` it can only `SELECT` and `INSERT`; it cannot `UPDATE`, `DELETE` or `TRUNCATE`. A trigger blocks those operations for the owner too. Be precise about what that buys: no *application code path* can alter the audit trail. A database owner can still disable the trigger, so this is not DBA-proof, and the migration says so.

### 6.1 `internal/store/migrations/00002_app_role_environments.sql` (new)

Creates the `ephematool_app` role (idempotently; the password is **dev-only**, production sets its own with `ALTER ROLE`), grants it the Phase 1 `users` table, and creates `environments`. The `status` CHECK allows only four states today; Phase 3 adds `destroying`/`destroyed` by replacing that constraint in one migration. The `id` CHECK (`^pv-[a-z0-9]{10}$`) makes a malformed namespace name impossible to store.

`internal/store/migrations/00002_app_role_environments.sql`

```sql
-- +goose Up

-- The runtime role. The compose/owner user keeps migrating (MIGRATE_DATABASE_URL);
-- the API and worker connect as ephematool_app (DATABASE_URL) with only the
-- grants below (D-24). The password is DEV-ONLY: outside local dev, create the
-- role with a real secret and run ALTER ROLE out of band; this block then skips.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'ephematool_app') THEN
    CREATE ROLE ephematool_app LOGIN PASSWORD 'ephematool_app';
  END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO ephematool_app;
GRANT SELECT, INSERT, UPDATE ON users TO ephematool_app;

create table environments (
    id                    text primary key check (id ~ '^pv-[a-z0-9]{10}$'),
    owner_github_id       bigint not null,
    owner_login           text not null,
    repo                  text not null,
    ref_type              text not null check (ref_type in ('branch', 'pr', 'sha')),
    ref                   text not null,
    sha                   text not null check (sha ~ '^[0-9a-f]{40}$'),
    image                 text not null,
    template              text not null,
    chart_version         text not null,
    size                  text not null check (size in ('S', 'M', 'L')),
    "values"              jsonb not null default '{}',
    -- Phase 3 adds 'destroying' and 'destroyed' by replacing this CHECK in one migration.
    status                text not null default 'requested'
                          check (status in ('requested', 'provisioning', 'running', 'failed')),
    failure_reason        text not null default '',
    correlation_id        text not null,
    traceparent           text not null,
    trace_id              text not null,
    run_id                bigint,
    run_html_url          text not null default '',
    run_etag              text not null default '',
    run_conclusion        text not null default '',
    run_concluded_at      timestamptz,
    dispatch_attempted_at timestamptz,
    dispatched_at         timestamptz,
    provision_deadline_at timestamptz,
    running_at            timestamptz,
    created_at            timestamptz not null default now(),
    updated_at            timestamptz not null default now(),
    expires_at            timestamptz not null
);

create index environments_owner_created_idx on environments (owner_github_id, created_at desc);
-- The sweeper scans only unfinished rows, so keep that index tiny.
create index environments_unfinished_idx on environments (created_at)
    where status in ('requested', 'provisioning');

REVOKE ALL ON environments FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON environments TO ephematool_app;

-- +goose Down
drop table environments;
REVOKE SELECT, INSERT, UPDATE ON users FROM ephematool_app;
-- The role itself is kept: other databases or sessions may still reference it.
```
### 6.2 `internal/store/migrations/00003_audit_events.sql` (new)

`internal/store/migrations/00003_audit_events.sql`

```sql
-- +goose Up
-- Append-only audit log (D-19, D-24). The app role gets SELECT and INSERT only.
-- A row trigger blocks UPDATE/DELETE and a statement trigger blocks TRUNCATE,
-- even for the table owner. The owner *can* still ALTER TABLE ... DISABLE
-- TRIGGER, so the guarantee is "no application code path", not "DBA-proof".
-- Never use ALTER DEFAULT PRIVILEGES in this project: it would hand UPDATE on
-- audit_events to the app role.
create table audit_events (
    id              uuid primary key default gen_random_uuid(),
    occurred_at     timestamptz not null default now(),
    actor_github_id bigint not null,
    actor_login     text not null,
    action          text not null,
    environment_id  text,
    outcome         text not null check (outcome in ('success', 'denied', 'rejected')),
    -- A stable machine code (not UI copy), for example not_creator.
    reason          text not null default '',
    trace_id        text not null default ''
);

-- Keyset paging: order by (occurred_at, id) descending.
create index audit_events_occurred_idx on audit_events (occurred_at desc, id desc);

-- +goose StatementBegin
CREATE FUNCTION audit_events_immutable() RETURNS trigger LANGUAGE plpgsql AS $f$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only' USING ERRCODE = 'restrict_violation';
END
$f$;
-- +goose StatementEnd

CREATE TRIGGER audit_events_no_mutation BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_immutable();

REVOKE ALL ON audit_events FROM PUBLIC;
GRANT SELECT, INSERT ON audit_events TO ephematool_app;

-- +goose Down
-- Dropping the table drops its triggers; the function goes with an explicit drop.
drop table audit_events;
drop function audit_events_immutable();
```
> **Never use `ALTER DEFAULT PRIVILEGES` in this project.** It would silently grant `UPDATE` on future tables, including `audit_events`, to the app role. Grant per table, explicitly, in each migration.

### 6.3 Queries

The `:execrows` queries are **compare-and-set** operations: each updates only when the row is in the expected state and returns how many rows it changed. `0` means "someone else already did it". Later phases rely on this to be safe under retries, sweeper re-runs and worker restarts.

`internal/store/queries/environments.sql`

```sql
-- name: InsertEnvironment :one
insert into environments (
    id, owner_github_id, owner_login, repo, ref_type, ref, sha, image,
    template, chart_version, size, "values", correlation_id, traceparent, trace_id, expires_at
) values (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
returning *;

-- name: GetEnvironment :one
select * from environments where id = $1;

-- name: ListEnvironmentsByOwner :many
select * from environments
where owner_github_id = $1
order by created_at desc, id desc
limit $2;

-- name: ListActiveEnvironments :many
-- Everything the sweeper must keep moving: not yet running and not failed.
select * from environments
where status in ('requested', 'provisioning')
order by created_at;

-- name: ClaimDispatch :execrows
-- Compare-and-set: only the first caller sees 1 row. A second caller (retry,
-- sweeper) sees 0 and must NOT post a second workflow dispatch.
update environments
set dispatch_attempted_at = now(), updated_at = now()
where id = $1 and status = 'requested' and dispatch_attempted_at is null;

-- name: MarkProvisioning :execrows
update environments
set status = 'provisioning', run_id = $2, run_html_url = $3,
    dispatched_at = now(), provision_deadline_at = $4, updated_at = now()
where id = $1 and status = 'requested';

-- name: UpdateRunETag :exec
update environments set run_etag = $2, updated_at = now() where id = $1;

-- name: MarkRunConcluded :execrows
-- Set once: the first poller to see the conclusion wins and emits the spans.
update environments
set run_conclusion = $2, run_concluded_at = now(), updated_at = now()
where id = $1 and run_concluded_at is null;

-- name: MarkRunning :execrows
update environments
set status = 'running', running_at = now(), updated_at = now()
where id = $1 and status = 'provisioning';

-- name: MarkFailed :execrows
update environments
set status = 'failed', failure_reason = $2, updated_at = now()
where id = $1 and status in ('requested', 'provisioning');
```
`internal/store/queries/audit.sql`

```sql
-- name: InsertAuditEvent :one
insert into audit_events (actor_github_id, actor_login, action, environment_id, outcome, reason, trace_id)
values ($1, $2, $3, $4, $5, $6, $7)
returning id, occurred_at;

-- name: ListAuditEvents :many
-- Filters are optional (NULL = no filter). The cursor is the (occurred_at, id)
-- of the last row of the previous page; row-value comparison keeps paging
-- stable when several events share a timestamp.
select a.id, a.occurred_at, a.actor_github_id, a.actor_login, a.action,
       a.environment_id, a.outcome, a.reason, a.trace_id,
       coalesce(u.avatar_url, '')::text as actor_avatar_url
from audit_events a
left join users u on u.github_id = a.actor_github_id
where (sqlc.narg('outcome')::text is null or a.outcome = sqlc.narg('outcome')::text)
  and (sqlc.narg('action')::text is null or a.action = sqlc.narg('action')::text)
  and (sqlc.narg('actor')::text is null or a.actor_login = sqlc.narg('actor')::text)
  and (sqlc.narg('cursor_at')::timestamptz is null
       or (a.occurred_at, a.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
order by a.occurred_at desc, a.id desc
limit sqlc.arg('row_limit')::int;
```
### 6.4 `internal/store/store.go` (replace)

`Migrate` now uses goose's Provider with a Postgres **session lock** (WR-11): two API replicas starting at once can no longer race on the version table, and the package-global goose state is no longer mutated.

`internal/store/store.go`

```go
// Package store owns Postgres access: pool, embedded goose migrations and the
// sqlc-generated queries.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
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
// prober and the migrator use it so those calls do not create traces.
func OpenUntraced(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 2 // the session locker holds one connection while goose uses another
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Migrate applies the embedded goose migrations under a Postgres advisory
// lock (WR-11), so two processes starting together cannot race. It must run
// with the OWNER connection (MIGRATE_DATABASE_URL), never the app role.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := MigrateVersion(ctx, pool)
	return err
}

// MigrateVersion is Migrate plus the resulting schema version, for logging.
func MigrateVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return 0, err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return 0, err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return 0, err
	}
	if _, err := provider.Up(ctx); err != nil {
		return 0, err
	}
	return provider.GetDBVersion(ctx)
}
```
### 6.5 `internal/store/BUILD.bazel` (edit)

Add the two new generated sources and the two migrations. `embedsrcs` is an explicit list (not gazelle-managed), so add each new migration by hand:

```python
go_library(
    name = "store",
    srcs = [
        "audit.sql.go",          # new (generated)
        "db.go",
        "environments.sql.go",   # new (generated)
        "generate.go",
        "models.go",
        "store.go",
        "users.sql.go",
    ],
    embedsrcs = [
        "migrations/00001_users.sql",
        "migrations/00002_app_role_environments.sql",
        "migrations/00003_audit_events.sql",
    ],
    # deps: add "@com_github_pressly_goose_v3//lock",
    ...
)
```

```bash
bazel run //:gen            # sqlc writes audit.sql.go, environments.sql.go, models.go
bazel run //:gazelle
bazel test //:check_gen //internal/store/...
```

> **Generated code is not hand-written.** The sqlc output (`internal/store/*.sql.go`, `models.go`) comes from `bazel run //:gen`; never edit it. Field names in later phases follow sqlc's conventions (`OwnerGithubID`, `RunID pgtype.Int8`, `TraceID`, `RunHtmlUrl`). If a later compile error mentions a field name, open the generated `models.go` and use the name that is there.

### 6.6 Prove the grants yourself, without Docker (optional)

If you have a local Postgres (any version 14+), this exercises exactly the SQL above. It is how these migrations were checked.

```bash
createdb t1
for f in internal/store/migrations/0000{1,2,3}_*.sql; do
  sed '/^-- +goose Down/,$d' "$f" | psql -v ON_ERROR_STOP=1 -q t1
done
APP="psql -q -U ephematool_app t1"
$APP -c "insert into audit_events(actor_github_id,actor_login,action,outcome) values (1,'x','environment.create','success')"   # ok
$APP -c "update audit_events set reason='x'"     # ERROR: permission denied for table audit_events
$APP -c "delete from audit_events"               # ERROR: permission denied
psql -q t1 -c "delete from audit_events"         # ERROR: audit_events is append-only  (owner, blocked by trigger)
psql -q t1 -c "truncate audit_events"            # ERROR: audit_events is append-only
```

### 6.7 The two-role test harness

`internal/testenv` starts Postgres and Redis containers **once per test binary**, migrates as the owner, and hands tests a pool for each role. Tests that write audit rows must use unique actors, because `audit_events` can never be truncated.

`internal/testenv/env.go`

```go
//go:build integration

// Package testenv starts real Postgres and Redis containers once per test
// binary and exposes them with the production role split: the owner migrates,
// the ephematool_app role runs queries. Tests that write audit_events must use
// unique actors, because that table can never be truncated.
package testenv

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/ephematool/ephematool/internal/store"
)

// Env is the shared infrastructure for one test.
type Env struct {
	OwnerURL, AppURL string
	Owner, App       *pgxpool.Pool  // owner (migrations, DDL checks) and app role
	Queries          *store.Queries // bound to the app role
	Redis            *redis.Client  // FLUSHALL on every Start
	RedisAddr        string
}

var shared struct {
	once      sync.Once
	ownerURL  string
	appURL    string
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
	if shared.ownerURL, shared.err = pg.ConnectionString(ctx, "sslmode=disable"); shared.err != nil {
		return
	}
	// Migrate once, as the owner. This also creates the ephematool_app role.
	pool, err := store.OpenUntraced(ctx, shared.ownerURL)
	if err != nil {
		shared.err = err
		return
	}
	defer pool.Close()
	if shared.err = store.Migrate(ctx, pool); shared.err != nil {
		return
	}
	shared.appURL, shared.err = WithCredentials(shared.ownerURL, "ephematool_app", "ephematool_app")
	if shared.err != nil {
		return
	}
	rc, err := tcredis.Run(ctx, "redis:8.8-alpine")
	if err != nil {
		shared.err = err
		return
	}
	uri, err := rc.ConnectionString(ctx)
	if err != nil {
		shared.err = err
		return
	}
	opt, err := redis.ParseURL(uri)
	if err != nil {
		shared.err = err
		return
	}
	shared.redisAddr = opt.Addr
}

// WithCredentials swaps the user and password of a Postgres URL.
func WithCredentials(dsn, user, password string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}

// WithDatabase swaps the database name of a Postgres URL.
func WithDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// Start returns the shared environment, skipping the test when Docker is not
// available. Redis is flushed so tests do not see each other's keys.
func Start(t *testing.T) *Env {
	t.Helper()
	shared.once.Do(startContainers)
	if shared.err != nil {
		t.Skipf("containers unavailable: %v", shared.err)
	}
	ctx := context.Background()
	owner, err := store.Open(ctx, shared.ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	app, err := store.Open(ctx, shared.appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	rdb := redis.NewClient(&redis.Options{Addr: shared.redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	return &Env{
		OwnerURL: shared.ownerURL, AppURL: shared.appURL,
		Owner: owner, App: app, Queries: store.New(app),
		Redis: rdb, RedisAddr: shared.redisAddr,
	}
}
```
### 6.8 Integration tests for the schema

`internal/store/migrate_integration_test.go`

```go
//go:build integration

package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/internal/testenv"
)

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestMigrateConcurrent(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	name := fmt.Sprintf("migrate_race_%d", time.Now().UnixNano())
	if _, err := env.Owner.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	dsn, _ := testenv.WithDatabase(env.OwnerURL, name)

	versions := make([]int64, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range versions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool, err := store.OpenUntraced(ctx, dsn)
			if err != nil {
				errs[i] = err
				return
			}
			defer pool.Close()
			versions[i], errs[i] = store.MigrateVersion(ctx, pool)
		}()
	}
	wg.Wait()
	for i := range versions {
		if errs[i] != nil || versions[i] != 3 {
			t.Errorf("migrator %d: version %d err %v, want version 3 and no error", i, versions[i], errs[i])
		}
	}
}

func TestAppRoleGrants(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	if _, err := env.App.Exec(ctx, `insert into audit_events (actor_github_id, actor_login, action, outcome)
		values (9001, 'grants-test', 'environment.create', 'success')`); err != nil {
		t.Fatalf("app insert into audit_events: %v", err)
	}
	denied := []string{
		"update audit_events set reason = 'x'",
		"delete from audit_events",
		"truncate audit_events",
		"select * from goose_db_version",
	}
	for _, q := range denied {
		if _, err := env.App.Exec(ctx, q); sqlState(err) != "42501" {
			t.Errorf("%q as app role: SQLSTATE %q, want 42501 (permission denied): %v", q, sqlState(err), err)
		}
	}
	if _, err := env.App.Exec(ctx, "insert into users (github_id, login) values (9002, 'grants') on conflict do nothing"); err != nil {
		t.Errorf("app insert into users: %v", err)
	}
	if _, err := env.App.Exec(ctx, "update users set login = 'grants2' where github_id = 9002"); err != nil {
		t.Errorf("app update users: %v", err)
	}
}

func TestOwnerBlockedByTrigger(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	for _, q := range []string{"delete from audit_events", "truncate audit_events"} {
		_, err := env.Owner.Exec(ctx, q)
		if err == nil || !strings.Contains(err.Error(), "audit_events is append-only") {
			t.Errorf("%q as owner: err = %v, want the append-only trigger error", q, err)
		}
	}
}

func insertEnv(t *testing.T, env *testenv.Env, id string) {
	t.Helper()
	_, err := env.App.Exec(context.Background(), `insert into environments
		(id, owner_github_id, owner_login, repo, ref_type, ref, sha, image, template, chart_version, size,
		 correlation_id, traceparent, trace_id, expires_at)
		values ($1, 1, 'o', 'a/b', 'branch', 'main', repeat('a', 40), 'img', 'web-app', '0.1.0', 'S', 'c', 't', 'tid', now())`, id)
	if err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func TestEnvironmentCAS(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	id := fmt.Sprintf("pv-%010d", time.Now().UnixNano()%1e10)
	insertEnv(t, env, id)
	q := env.Queries

	if n, err := q.MarkRunning(ctx, id); err != nil || n != 0 {
		t.Fatalf("MarkRunning on a requested row: n=%d err=%v, want 0", n, err)
	}
	if n, _ := q.ClaimDispatch(ctx, id); n != 1 {
		t.Fatalf("first ClaimDispatch = %d, want 1", n)
	}
	if n, _ := q.ClaimDispatch(ctx, id); n != 0 {
		t.Fatalf("second ClaimDispatch = %d, want 0 (no double dispatch)", n)
	}
	args := store.MarkProvisioningParams{ID: id, RunID: pgInt8(42), RunHtmlUrl: "http://run", ProvisionDeadlineAt: pgTime(time.Now().Add(time.Minute))}
	if n, _ := q.MarkProvisioning(ctx, args); n != 1 {
		t.Fatalf("first MarkProvisioning = %d, want 1", n)
	}
	if n, _ := q.MarkProvisioning(ctx, args); n != 0 {
		t.Fatalf("second MarkProvisioning = %d, want 0", n)
	}
	if n, _ := q.MarkRunning(ctx, id); n != 1 {
		t.Fatalf("MarkRunning = %d, want 1", n)
	}
	if n, _ := q.MarkFailed(ctx, store.MarkFailedParams{ID: id, FailureReason: "late"}); n != 0 {
		t.Fatalf("MarkFailed after running = %d, want 0 (forward-only)", n)
	}
	if _, err := env.App.Exec(ctx, "update environments set status = 'destroyed' where id = $1", id); sqlState(err) != "23514" {
		t.Fatalf("status 'destroyed' must violate the CHECK (23514), got %q", sqlState(err))
	}
}

func TestEnvironmentIDCheck(t *testing.T) {
	env := testenv.Start(t)
	for _, id := range []string{"pv-UPPER12345", "evil", "pv-short"} {
		_, err := env.App.Exec(context.Background(), `insert into environments
			(id, owner_github_id, owner_login, repo, ref_type, ref, sha, image, template, chart_version, size,
			 correlation_id, traceparent, trace_id, expires_at)
			values ($1, 1, 'o', 'a/b', 'branch', 'main', repeat('a', 40), 'img', 'web-app', '0.1.0', 'S', 'c', 't', 'tid', now())`, id)
		if sqlState(err) != "23514" {
			t.Errorf("id %q: SQLSTATE %q, want 23514 (CHECK violation)", id, sqlState(err))
		}
	}
}
```
`internal/store/helpers_integration_test.go`

```go
//go:build integration

package store_test

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgInt8(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: true} }

func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
```
Add the package to the integration suite in root `BUILD.bazel`:

```python
test_suite(
    name = "test_integration",
    tests = [
        "//internal/auth:auth_test",
        "//internal/store:store_test",     # new
        "//internal/audit:audit_test",     # new (section 7)
    ],
)
```

```bash
bazel run //:gazelle
bazel test --config=integration //internal/store:store_test //internal/auth:auth_test
```

Expected: five store tests pass (`TestMigrateConcurrent`, `TestAppRoleGrants`, `TestOwnerBlockedByTrigger`, `TestEnvironmentCAS`, `TestEnvironmentIDCheck`) and the Phase 1 auth tests still pass (`store.Migrate` keeps its signature).

---

## 7. `internal/audit` (plan 02-04, task 2)

Every create attempt is recorded, including the ones that never create anything: **success**, **denied** (the caller is not allowed to try) and **rejected** (allowed, but the request was invalid). The stored `reason` is a stable machine code, never UI copy, and tokens are never stored.

`Record` accepts a `*store.Queries`, so passing a transaction-bound one (`q.WithTx(tx)`) makes the audit row commit atomically with the change it describes. `List` pages with an opaque keyset cursor over `(occurred_at, id)`; it fetches one extra row to know whether another page exists.

`internal/audit/audit.go`

```go
// Package audit records and pages the append-only audit log (D-19). Every
// create attempt is recorded: success, denied (not allowed to try) or rejected
// (allowed but the request was invalid). Reasons are stable machine codes; the
// UI maps them to copy. Never store tokens or the traceparent header here.
package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/store"
)

// Actions and outcomes.
const (
	ActionEnvironmentCreate = "environment.create"

	OutcomeSuccess  = "success"
	OutcomeDenied   = "denied"
	OutcomeRejected = "rejected"
)

// Reason codes (stored, stable). The UI owns the human copy.
const (
	ReasonNotCreator        = "not_creator"
	ReasonRepoNotAllowed    = "repo_not_allowed"
	ReasonRefNotFound       = "ref_not_found"
	ReasonRefInvalid        = "ref_invalid"
	ReasonPRNotEligible     = "pr_not_eligible"
	ReasonImageNotFound     = "image_not_found"
	ReasonSchemaViolation   = "schema_violation"
	ReasonTemplateRequired  = "template_required"
	ReasonSizeRequired      = "size_required"
	ReasonGitHubUnavailable = "github_unavailable"
)

// Event is one audit entry to write.
type Event struct {
	ActorGithubID int64
	ActorLogin    string
	Action        string
	EnvironmentID string // empty when no environment exists (denied/rejected)
	Outcome       string
	Reason        string
	TraceID       string // filled from ctx when empty
}

// Record appends an event. Pass a transaction-bound *store.Queries to make the
// audit row commit atomically with the change it describes.
func Record(ctx context.Context, q *store.Queries, e Event) error {
	e.TraceID = traceIDFrom(ctx, e.TraceID)
	_, err := q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ActorGithubID: e.ActorGithubID,
		ActorLogin:    e.ActorLogin,
		Action:        e.Action,
		EnvironmentID: pgtype.Text{String: e.EnvironmentID, Valid: e.EnvironmentID != ""},
		Outcome:       e.Outcome,
		Reason:        e.Reason,
		TraceID:       e.TraceID,
	})
	if err != nil {
		return fmt.Errorf("audit insert: %w", err)
	}
	return nil
}

// traceIDFrom returns explicit when set, else the trace id of the span in ctx.
func traceIDFrom(ctx context.Context, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// Filter narrows List. Empty fields mean "no filter".
type Filter struct {
	Outcome, Action, Actor, Cursor string
	Limit                          int // default 50, max 100
}

// Item is one audit row as returned to callers.
type Item struct {
	ID             string
	OccurredAt     time.Time
	ActorGithubID  int64
	ActorLogin     string
	ActorAvatarURL string
	Action         string
	EnvironmentID  string
	Outcome        string
	Reason         string
	TraceID        string
}

// Page is one page of items, newest first.
type Page struct {
	Events     []Item
	NextCursor string // empty on the last page
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// List returns one page, newest first, with an opaque keyset cursor.
func List(ctx context.Context, q *store.Queries, f Filter) (Page, error) {
	limit := normalizeLimit(f.Limit)
	p := store.ListAuditEventsParams{
		Outcome: text(f.Outcome), Action: text(f.Action), Actor: text(f.Actor),
		// Fetch one extra row to learn whether another page exists.
		RowLimit: int32(limit + 1),
	}
	if f.Cursor != "" {
		at, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return Page{}, err
		}
		p.CursorAt = pgtype.Timestamptz{Time: at, Valid: true}
		p.CursorID = pgtype.UUID{Bytes: id, Valid: true}
	}
	rows, err := q.ListAuditEvents(ctx, p)
	if err != nil {
		return Page{}, fmt.Errorf("audit list: %w", err)
	}
	var page Page
	for i, r := range rows {
		if i == limit { // the extra row only proves there is a next page
			last := rows[limit-1]
			page.NextCursor = encodeCursor(last.OccurredAt.Time, last.ID.Bytes)
			break
		}
		page.Events = append(page.Events, Item{
			ID: uuidString(r.ID.Bytes), OccurredAt: r.OccurredAt.Time,
			ActorGithubID: r.ActorGithubID, ActorLogin: r.ActorLogin, ActorAvatarURL: r.ActorAvatarUrl,
			Action: r.Action, EnvironmentID: r.EnvironmentID.String, Outcome: r.Outcome,
			Reason: r.Reason, TraceID: r.TraceID,
		})
	}
	if page.Events == nil {
		page.Events = []Item{}
	}
	return page, nil
}
```
`internal/audit/cursor.go`

```go
package audit

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBadCursor is returned for a cursor that is not one this package issued.
var ErrBadCursor = errors.New("audit: invalid cursor")

const (
	defaultLimit = 50
	maxLimit     = 100
)

// normalizeLimit applies the default (50) and the cap (100).
func normalizeLimit(n int) int {
	switch {
	case n <= 0:
		return defaultLimit
	case n > maxLimit:
		return maxLimit
	}
	return n
}

// uuidString formats 16 bytes as a canonical UUID.
func uuidString(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseUUID is the inverse of uuidString.
func parseUUID(s string) ([16]byte, error) {
	var out [16]byte
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != 16 || len(s) != 36 {
		return out, ErrBadCursor
	}
	copy(out[:], raw)
	return out, nil
}

// encodeCursor makes the opaque cursor "<occurred_at RFC3339Nano>|<uuid>"
// (base64url). It is opaque to clients, not secret: it only names a position.
func encodeCursor(at time.Time, id [16]byte) string {
	raw := at.UTC().Format(time.RFC3339Nano) + "|" + uuidString(id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a cursor, returning ErrBadCursor for anything else.
func decodeCursor(c string) (time.Time, [16]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	tsPart, idPart, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	at, err := time.Parse(time.RFC3339Nano, tsPart)
	if err != nil {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	id, err := parseUUID(idPart)
	if err != nil {
		return time.Time{}, [16]byte{}, ErrBadCursor
	}
	return at, id, nil
}
```
Tests: the cursor and limit rules and the trace-id rule run without a database; the rest runs against the containers.

`internal/audit/cursor_test.go`

```go
package audit

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 12, 20, 10, 36, 123456000, time.UTC) // microsecond precision, like Postgres
	id := [16]byte{0xf7, 0x7e, 0xcb, 0x51, 0xfe, 0x28, 0x41, 0xde, 0xbc, 0x31, 0xae, 0xd5, 0x44, 0x61, 0x3d, 0x8a}
	gotAt, gotID, err := decodeCursor(encodeCursor(at, id))
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(at) || gotID != id {
		t.Fatalf("round trip = %v %x, want %v %x", gotAt, gotID, at, id)
	}
}

func TestCursorRejectsTampering(t *testing.T) {
	good := encodeCursor(time.Now(), [16]byte{1})
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, c := range map[string]string{
		"not base64":   "!!!",
		"no separator": enc("2026-10-12T00:00:00Z"),
		"bad time":     enc("yesterday|00000000-0000-0000-0000-000000000001"),
		"bad uuid":     enc("2026-10-12T00:00:00Z|nope"),
		"short uuid":   enc("2026-10-12T00:00:00Z|0001"),
		"truncated":    good[:len(good)-4],
	} {
		if _, _, err := decodeCursor(c); !errors.Is(err, ErrBadCursor) {
			t.Errorf("%s: err = %v, want ErrBadCursor", name, err)
		}
	}
}

func TestNormalizeLimit(t *testing.T) {
	for in, want := range map[int]int{0: 50, -3: 50, 1: 1, 100: 100, 500: 100} {
		if got := normalizeLimit(in); got != want {
			t.Errorf("normalizeLimit(%d) = %d, want %d", in, got, want)
		}
	}
}
```
`internal/audit/audit_test.go`

```go
package audit

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// traceIDFrom is the trace-id rule Record uses, tested without a database.
// (Record itself is covered by the integration test.)
func TestTraceIDComesFromSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter()))
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()
	want := span.SpanContext().TraceID().String()
	if got := traceIDFrom(ctx, ""); got != want {
		t.Fatalf("traceIDFrom = %q, want %q", got, want)
	}
	if got := traceIDFrom(ctx, "explicit"); got != "explicit" {
		t.Fatalf("an explicit trace id must win, got %q", got)
	}
	if got := traceIDFrom(context.Background(), ""); got != "" {
		t.Fatalf("no span, want empty, got %q", got)
	}
}
```
`internal/audit/audit_integration_test.go`

```go
//go:build integration

package audit_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ephematool/ephematool/internal/audit"
	"github.com/ephematool/ephematool/internal/testenv"
)

// uniqueActor isolates this test's rows: audit_events can never be truncated.
func uniqueActor(t *testing.T) (int64, string) {
	n := time.Now().UnixNano()
	return n % 1e12, fmt.Sprintf("actor-%d", n)
}

func TestRecordAndList(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	id, login := uniqueActor(t)

	events := []audit.Event{
		{Outcome: audit.OutcomeSuccess, Reason: "", EnvironmentID: "pv-aaaaaaaaaa"},
		{Outcome: audit.OutcomeDenied, Reason: audit.ReasonNotCreator},
		{Outcome: audit.OutcomeRejected, Reason: audit.ReasonRefNotFound},
		{Outcome: audit.OutcomeDenied, Reason: audit.ReasonRepoNotAllowed},
		{Outcome: audit.OutcomeSuccess, EnvironmentID: "pv-bbbbbbbbbb"},
	}
	for _, e := range events {
		e.ActorGithubID, e.ActorLogin, e.Action, e.TraceID = id, login, audit.ActionEnvironmentCreate, "0123456789abcdef0123456789abcdef"
		if err := audit.Record(ctx, env.Queries, e); err != nil {
			t.Fatal(err)
		}
	}

	all, err := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Limit: 50})
	if err != nil || len(all.Events) != 5 || all.NextCursor != "" {
		t.Fatalf("List = %d events cursor %q err %v, want 5 and no cursor", len(all.Events), all.NextCursor, err)
	}
	if all.Events[0].EnvironmentID != "pv-bbbbbbbbbb" || all.Events[0].TraceID == "" {
		t.Errorf("newest first with trace id expected, got %+v", all.Events[0])
	}
	denied, _ := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Outcome: audit.OutcomeDenied})
	if len(denied.Events) != 2 {
		t.Errorf("outcome=denied returned %d, want 2", len(denied.Events))
	}
	none, _ := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Action: "nope"})
	if len(none.Events) != 0 {
		t.Errorf("unknown action returned %d, want 0", len(none.Events))
	}

	// Paging: 5 events, limit 2 -> 2, 2, 1 with an empty cursor at the end.
	var sizes []int
	cursor := ""
	for i := 0; i < 5; i++ {
		page, err := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(page.Events))
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if fmt.Sprint(sizes) != "[2 2 1]" {
		t.Errorf("page sizes = %v, want [2 2 1]", sizes)
	}
	if _, err := audit.List(ctx, env.Queries, audit.Filter{Cursor: "garbage"}); err == nil {
		t.Error("garbage cursor must return ErrBadCursor")
	}
}

func TestAvatarFromUsers(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	id, login := uniqueActor(t)
	if _, err := env.App.Exec(ctx, "insert into users (github_id, login, avatar_url) values ($1, $2, 'http://a/b.png')", id, login); err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(ctx, env.Queries, audit.Event{ActorGithubID: id, ActorLogin: login,
		Action: audit.ActionEnvironmentCreate, Outcome: audit.OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	page, _ := audit.List(ctx, env.Queries, audit.Filter{Actor: login})
	if len(page.Events) != 1 || page.Events[0].ActorAvatarURL != "http://a/b.png" {
		t.Fatalf("avatar not joined from users: %+v", page.Events)
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/audit/...
bazel test --config=integration //:test_integration
```

### 7.1 `cmd/api/main.go` (replace)

Three changes. Migrations run with the **owner** connection (`MigrationURL()`) and the pool is closed afterwards; the runtime pool is opened with `DATABASE_URL` (the app role) only after migrating; the GitHub App client is created if configured, and a missing App is a warning rather than a crash. One subtle line: the `TeamChecker` is assigned only when the client is non-nil, to avoid a typed-nil value inside the interface.

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
	"github.com/ephematool/ephematool/internal/ghapp"
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
	return runMigrations(ctx, cfg)
}

// runMigrations applies migrations with the OWNER connection (D-24). The API's
// runtime pool uses the restricted ephematool_app role and can never run DDL.
func runMigrations(ctx context.Context, cfg config.Config) error {
	pool, err := store.OpenUntraced(ctx, cfg.MigrationURL())
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer pool.Close()
	version, err := store.MigrateVersion(ctx, pool)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	slog.InfoContext(ctx, "db.migrated", "version", version) // never log the URL
	return nil
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

	// Migrate first (owner role), then open the runtime pool (app role).
	if cfg.MigrateOnStart {
		if err := runMigrations(ctx, cfg); err != nil {
			return err
		}
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	queries := store.New(pool)
	sessions := auth.NewSessionManager(rdb, cfg)

	// GitHub App client: optional. Without it creator teams cannot be checked
	// and (from 2b) creating environments is disabled with a clear message.
	gh, err := ghapp.New(cfg)
	switch {
	case errors.Is(err, ghapp.ErrNotConfigured):
		slog.WarnContext(ctx, "ghapp.not_configured", "hint", "set GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, GITHUB_APP_PRIVATE_KEY_FILE")
		gh = nil
	case err != nil:
		return err
	}
	authHandlers := auth.NewHandlers(cfg, sessions, queries)
	if gh != nil { // avoid a typed-nil in the TeamChecker interface
		authHandlers.Roles = auth.NewRoleResolver(cfg, gh)
	}

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
		Sessions: sessions, Queries: queries, Auth: authHandlers,
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
---

## 8. GitHub App client: `internal/ghapp` (plan 02-05)

The platform authenticates as a **GitHub App**, not as a user. An RS256 App JWT (issuer = App id, `iat` = now − 60 s, `exp` = now + 9 min) is exchanged for an installation token, cached until 5 minutes before expiry and injected into every API call by a `RoundTripper`. The private key is read from a mounted file (never an env value, never logged).

**App permissions (least privilege).** Create the App with exactly these, installed on the environments repo and each allowed repo:

| Permission | Level | Needed for |
|---|---|---|
| Actions | read & write | dispatch (write); runs and jobs (read) on the environments repo |
| Contents | read | resolve branch and commit on allowed repos |
| Pull requests | read | PR number → head sha; fork detection |
| Metadata | read | mandatory |
| Organization members | read | request-time team lookups for `CREATOR_TEAMS` |

No Administration (the platform never registers runners), no Packages (the App cannot read GHCR anyway; the registry check is anonymous, D-23), no Workflows.

```bash
go get github.com/golang-jwt/jwt/v5@v5.3.1     # pinned in versions.env (GO_JWT)
bazel mod tidy
```

### 8.1 Validation and sentinel errors (no network)

`ValidateRefInput` runs **before any HTTP call**: go-github builds request paths from these values, so they must be boring. A test proves that invalid input produces zero requests.

`internal/ghapp/validate.go`

```go
package ghapp

import (
	"errors"
	"regexp"
	"strings"
)

// Sentinel errors. Callers (internal/envs) map them to audited rejections.
var (
	ErrNotConfigured    = errors.New("ghapp: GitHub App is not configured")
	ErrRefInvalid       = errors.New("ghapp: ref is not valid for its type")
	ErrRefNotFound      = errors.New("ghapp: ref not found")
	ErrPRNotEligible    = errors.New("ghapp: pull request is not eligible")
	ErrImageNotFound    = errors.New("ghapp: image not found")
	ErrDispatchRejected = errors.New("ghapp: workflow dispatch rejected")
)

// RefType says how a user-supplied ref is interpreted.
type RefType string

// The three ways to point at code (ENV-01).
const (
	RefBranch RefType = "branch"
	RefPR     RefType = "pr"
	RefSHA    RefType = "sha"
)

var (
	branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	prRe     = regexp.MustCompile(`^[0-9]{1,9}$`)
	shaRe    = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// ValidateRefInput rejects a ref before any network call is made. It is the
// first defence against SSRF/path injection through the ref field: go-github
// builds request paths from these values, so they must be boring.
func ValidateRefInput(t RefType, ref string) error {
	switch t {
	case RefBranch:
		if !branchRe.MatchString(ref) || strings.Contains(ref, "..") ||
			strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "-") {
			return ErrRefInvalid
		}
	case RefPR:
		if !prRe.MatchString(ref) {
			return ErrRefInvalid
		}
	case RefSHA:
		if !shaRe.MatchString(ref) {
			return ErrRefInvalid
		}
	default:
		return ErrRefInvalid
	}
	return nil
}

// ImageTag renders IMAGE_TAG_FORMAT for a commit: {sha} is the full 40 chars,
// {sha7} the first seven (D-23).
func ImageTag(format, sha string) string {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	return strings.NewReplacer("{sha7}", short, "{sha}", sha).Replace(format)
}
```
### 8.2 Client, token cache and team membership

`internal/ghapp/ghapp.go`

```go
// Package ghapp is the platform's GitHub client. It authenticates as a GitHub
// App (not as a user): an RS256 App JWT is exchanged for a cached installation
// token, which signs every API call. It never imports internal/envs or
// internal/auth; those packages define the small interfaces this client
// satisfies.
package ghapp

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v92/github"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ephematool/ephematool/internal/config"
)

// Client talks to GitHub as the platform's App installation.
type Client struct {
	cfg   config.Config
	key   *rsa.PrivateKey
	gh    *github.Client // authenticated with the installation token
	app   *github.Client // authenticated with the App JWT (token minting only)
	owner string         // environments repo owner
	repo  string         // environments repo name
	tok   *tokenCache
	now   func() time.Time
	hc    *http.Client // plain client for the GHCR registry (no GitHub credentials!)

	extraInputs map[string]any // tests only: see Dispatch
}

// New reads the private key from GITHUB_APP_PRIVATE_KEY_FILE (a mounted file,
// never an env value) and builds the client. It returns ErrNotConfigured when
// the App settings are incomplete, so callers can degrade (create is disabled)
// instead of crashing.
func New(cfg config.Config) (*Client, error) {
	if !cfg.GitHubAppConfigured() {
		return nil, ErrNotConfigured
	}
	pem, err := os.ReadFile(cfg.GitHubAppPrivateKeyFile)
	if err != nil {
		// Report the path, never the content.
		return nil, fmt.Errorf("ghapp: read private key %s: %w", cfg.GitHubAppPrivateKeyFile, err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pem)
	if err != nil {
		return nil, fmt.Errorf("ghapp: parse private key: %w", err)
	}
	return NewWithKey(cfg, key), nil
}

// NewWithKey is New with an already-parsed key (tests generate one in memory).
func NewWithKey(cfg config.Config, key *rsa.PrivateKey) *Client {
	c := &Client{cfg: cfg, key: key, now: time.Now}
	c.owner, c.repo, _ = strings.Cut(cfg.EnvironmentsRepo, "/")
	c.tok = &tokenCache{}

	traced := func(rt http.RoundTripper) http.RoundTripper { return otelhttp.NewTransport(rt) }
	apiURL := cfg.GitHubAPIURL
	if !strings.HasSuffix(apiURL, "/") {
		apiURL += "/"
	}
	mk := func(auth func(context.Context) (string, error)) *github.Client {
		hc := &http.Client{
			Timeout:   20 * time.Second,
			Transport: traced(&bearerTransport{base: http.DefaultTransport, token: auth}),
		}
		gh, err := github.NewClient(github.WithHTTPClient(hc), github.WithURLs(&apiURL, nil))
		if err != nil {
			panic(err) // only fails on a malformed base URL, which config validation prevents
		}
		return gh
	}
	c.app = mk(func(context.Context) (string, error) { return c.appJWT() })
	c.gh = mk(c.installationToken)
	c.hc = &http.Client{Timeout: 15 * time.Second, Transport: traced(http.DefaultTransport)}
	return c
}

// bearerTransport injects "Authorization: Bearer <token>" on every request.
type bearerTransport struct {
	base  http.RoundTripper
	token func(context.Context) (string, error)
}

func (b *bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tok, err := b.token(r.Context())
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context()) // RoundTrippers must not mutate the caller's request
	r.Header.Set("Authorization", "Bearer "+tok)
	return b.base.RoundTrip(r)
}

// statusOf extracts an HTTP status from a go-github response or error.
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

// RetryAfter reports how long to wait after a GitHub rate-limit error. The
// worker reschedules instead of failing the environment.
func RetryAfter(err error) (time.Duration, bool) {
	var rl *github.RateLimitError
	if errors.As(err, &rl) {
		d := time.Until(rl.Rate.Reset.Time)
		if d < time.Second {
			d = time.Second
		}
		return d, true
	}
	var ab *github.AbuseRateLimitError
	if errors.As(err, &ab) {
		if ab.RetryAfter != nil {
			return *ab.RetryAfter, true
		}
		return time.Minute, true
	}
	return 0, false
}

// IsTeamMember reports whether login is an active member of org/slug. 404 means
// "not a member" (false, nil); other failures are errors so the caller can fail
// closed and log.
func (c *Client) IsTeamMember(ctx context.Context, org, slug, login string) (bool, error) {
	m, resp, err := c.gh.Teams.GetTeamMembershipBySlug(ctx, org, slug, login)
	if status := statusOf(resp, err); status == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return m.GetState() == "active", nil
}
```
`internal/ghapp/token.go`

```go
package ghapp

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// tokenCache holds one installation token. Concurrent callers share it.
type tokenCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

// refreshMargin: refresh when less than this remains, so a request never
// starts with a token that is about to die.
const refreshMargin = 5 * time.Minute

// appJWT signs the short-lived App JWT. iat is backdated 60s for clock skew;
// exp is 9 minutes (GitHub's maximum is 10).
func (c *Client) appJWT() (string, error) {
	now := c.now()
	claims := jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(c.cfg.GitHubAppID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(c.key)
}

// installationToken returns a cached token, minting a new one when needed.
func (c *Client) installationToken(ctx context.Context) (string, error) {
	c.tok.mu.Lock()
	defer c.tok.mu.Unlock()
	if c.tok.token != "" && c.now().Add(refreshMargin).Before(c.tok.expires) {
		return c.tok.token, nil
	}
	it, _, err := c.app.Apps.CreateInstallationToken(ctx, c.cfg.GitHubAppInstallationID, nil)
	if err != nil {
		return "", fmt.Errorf("ghapp: create installation token: %w", err)
	}
	c.tok.token = it.GetToken()
	c.tok.expires = it.GetExpiresAt().Time
	return c.tok.token, nil
}
```
### 8.3 Refs and the registry check

A PR is eligible only if it is **open and from the same repository**. A fork's `head.repo.full_name` differs, and a deleted fork has a `null` head repo; both are refused, because a fork's code must never be built and run on the platform. The image check is anonymous and returns a **digest-pinned** reference (`ghcr.io/org/repo@sha256:…`), so a re-pushed tag cannot change what runs.

`internal/ghapp/refs.go`

```go
package ghapp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func splitRepo(repo string) (owner, name string) {
	owner, name, _ = strings.Cut(repo, "/")
	return owner, name
}

// ResolveRef turns a branch, PR number or (short) sha into a full 40-char
// commit sha. Input is validated first, so invalid refs never reach GitHub.
func (c *Client) ResolveRef(ctx context.Context, repo string, t RefType, ref string) (string, error) {
	if err := ValidateRefInput(t, ref); err != nil {
		return "", err
	}
	owner, name := splitRepo(repo)
	switch t {
	case RefBranch:
		r, resp, err := c.gh.Git.GetRef(ctx, owner, name, "heads/"+ref)
		if statusOf(resp, err) == http.StatusNotFound {
			return "", ErrRefNotFound
		}
		if err != nil {
			return "", err
		}
		return r.GetObject().GetSHA(), nil

	case RefPR:
		n, _ := strconv.Atoi(ref)
		pr, resp, err := c.gh.PullRequests.Get(ctx, owner, name, n)
		if statusOf(resp, err) == http.StatusNotFound {
			return "", ErrRefNotFound
		}
		if err != nil {
			return "", err
		}
		// Only open PRs from the same repository: a fork's code must never be
		// built and run on the platform (fork head.repo differs; a deleted fork
		// has a nil head.repo).
		head := pr.GetHead()
		if pr.GetState() != "open" || head.GetRepo() == nil ||
			!strings.EqualFold(head.GetRepo().GetFullName(), repo) {
			return "", ErrPRNotEligible
		}
		return head.GetSHA(), nil

	default: // RefSHA: confirm the commit exists in this repo and expand it
		cm, resp, err := c.gh.Repositories.GetCommit(ctx, owner, name, ref, nil)
		if statusOf(resp, err) == http.StatusNotFound {
			return "", ErrRefNotFound
		}
		if err != nil {
			return "", err
		}
		return cm.GetSHA(), nil
	}
}
```
`internal/ghapp/image.go`

```go
package ghapp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// manifestAccept lists the manifest media types GHCR may answer with.
const manifestAccept = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

// ImageRef checks that ghcr.io/<org>/<repo>:<tag> exists using an anonymous
// registry token (D-23: no GitHub credential is sent to the registry) and
// returns the digest-pinned reference "ghcr.io/<org>/<repo>@sha256:...".
// Deploying by digest means a re-pushed tag cannot change what runs.
func (c *Client) ImageRef(ctx context.Context, repo, sha string) (string, error) {
	repo = strings.ToLower(repo)
	tag := ImageTag(c.cfg.ImageTagFormat, sha)
	base := strings.TrimRight(c.cfg.GHCRBaseURL, "/")
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", base, repo, url.PathEscape(tag))

	do := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", manifestAccept)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return c.hc.Do(req)
	}

	resp, err := do("")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		token, terr := c.registryToken(ctx, resp.Header.Get("WWW-Authenticate"), repo)
		if terr != nil {
			return "", terr
		}
		if resp, err = do(token); err != nil {
			return "", err
		}
		resp.Body.Close()
	}
	switch resp.StatusCode {
	case http.StatusOK:
		digest := resp.Header.Get("Docker-Content-Digest")
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
			return "", fmt.Errorf("ghapp: registry returned no usable digest for %s:%s", repo, tag)
		}
		// The reference always names ghcr.io: the registry URL may point at a
		// fake in dev/e2e, but the workflow only accepts ghcr.io images.
		return fmt.Sprintf("ghcr.io/%s@%s", repo, digest), nil
	case http.StatusNotFound, http.StatusForbidden:
		// 403 is what GHCR answers for a package that is private or absent.
		return "", ErrImageNotFound
	default:
		return "", fmt.Errorf("ghapp: registry answered %d for %s:%s", resp.StatusCode, repo, tag)
	}
}

// registryToken follows the Bearer challenge in WWW-Authenticate and fetches an
// anonymous pull token for repo.
func (c *Client) registryToken(ctx context.Context, challenge, repo string) (string, error) {
	realm := challengeParam(challenge, "realm")
	if realm == "" {
		return "", fmt.Errorf("ghapp: registry challenge has no realm")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("scope", "repository:"+repo+":pull")
	if svc := challengeParam(challenge, "service"); svc != "" {
		q.Set("service", svc)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ghapp: registry token endpoint answered %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("ghapp: registry token response unusable")
	}
	return out.Token, nil
}

// challengeParam extracts key="value" from a Bearer challenge header.
func challengeParam(header, key string) string {
	i := strings.Index(header, key+`="`)
	if i < 0 {
		return ""
	}
	rest := header[i+len(key)+2:]
	if j := strings.Index(rest, `"`); j >= 0 {
		return rest[:j]
	}
	return ""
}
```
### 8.4 Dispatch, correlation, ETag polling, jobs

The workflow has exactly 12 string inputs. Two GitHub behaviours shape this file:

- `workflow_dispatch` returns a run id only when `return_run_details` is set (API version 2022-11-28, the version go-github pins). When it does not, the result is **ambiguous**: the caller must find the run by correlation id (the workflow's `run-name` contains it) and **must not dispatch again**, because a dispatch is not idempotent.
- go-github has no per-call header option, so the conditional `GET` run (`If-None-Match`) is built by hand. A `304` does not count against the primary rate limit.

`internal/ghapp/actions.go`

```go
package ghapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
)

func decodeJSON(r io.Reader, v any) error { return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v) }

// DispatchInput is everything the provisioning workflow receives. The workflow
// has exactly 12 string inputs (GitHub allows 25): the shell never sees them
// except through env: and the first workflow step regex-validates each one.
type DispatchInput struct {
	EnvID, CorrelationID, Traceparent, Repo, SHA, Image string
	Template, ChartVersion, Size, ValuesB64, Owner      string
	ExpiresAt                                           int64 // unix seconds
}

// DispatchResult is the outcome of a dispatch. Ambiguous means GitHub accepted
// the request but returned no run id (HTTP 204): the caller must find the run
// by correlation id and must NOT dispatch again.
type DispatchResult struct {
	RunID     int64
	HTMLURL   string
	Ambiguous bool
}

// Dispatch starts the create-environment workflow on the environments repo.
// go-github pins API version 2022-11-28, under which run details are returned
// only when return_run_details is set; both shapes are handled.
func (c *Client) Dispatch(ctx context.Context, in DispatchInput) (DispatchResult, error) {
	inputs := map[string]any{
		"env_id": in.EnvID, "correlation_id": in.CorrelationID, "traceparent": in.Traceparent,
		"repo": in.Repo, "sha": in.SHA, "image": in.Image, "template": in.Template,
		"chart_version": in.ChartVersion, "size": in.Size, "values_b64": in.ValuesB64,
		"owner": in.Owner, "expires_at": strconv.FormatInt(in.ExpiresAt, 10),
	}
	for k, v := range c.extraInputs { // test hook: lets a test exceed GitHub's 25-input limit
		inputs[k] = v
	}
	rd, resp, err := c.gh.Actions.CreateWorkflowDispatchEventByFileName(ctx, c.owner, c.repo,
		c.cfg.EnvironmentsWorkflow,
		github.CreateWorkflowDispatchEventRequest{
			Ref:              c.cfg.EnvironmentsRef,
			ReturnRunDetails: github.Ptr(true),
			Inputs:           inputs,
		})
	status := statusOf(resp, err)
	if status == http.StatusUnprocessableEntity || status == http.StatusNotFound {
		return DispatchResult{}, fmt.Errorf("%w: HTTP %d", ErrDispatchRejected, status)
	}
	if err != nil {
		return DispatchResult{}, err
	}
	if rd != nil && rd.WorkflowRunID != nil && *rd.WorkflowRunID != 0 {
		return DispatchResult{RunID: *rd.WorkflowRunID, HTMLURL: rd.GetHTMLURL()}, nil
	}
	return DispatchResult{Ambiguous: true}, nil
}

// Run is the subset of a workflow run the platform uses.
type Run struct {
	ID                                        int64
	Status, Conclusion, HTMLURL, DisplayTitle string
	CreatedAt, RunStartedAt, UpdatedAt        time.Time
}

func toRun(r *github.WorkflowRun) Run {
	return Run{
		ID: r.GetID(), Status: r.GetStatus(), Conclusion: r.GetConclusion(),
		HTMLURL: r.GetHTMLURL(), DisplayTitle: r.GetDisplayTitle(),
		CreatedAt: r.GetCreatedAt().Time, RunStartedAt: r.GetRunStartedAt().Time, UpdatedAt: r.GetUpdatedAt().Time,
	}
}

// FindRunByCorrelation lists recent workflow_dispatch runs and returns the one
// whose title (run-name: "create <env_id> <correlation_id>") contains the id.
func (c *Client) FindRunByCorrelation(ctx context.Context, correlationID string, since time.Time) (Run, bool, error) {
	runs, _, err := c.gh.Actions.ListRepositoryWorkflowRuns(ctx, c.owner, c.repo, &github.ListWorkflowRunsOptions{
		Event:       "workflow_dispatch",
		Created:     ">=" + since.Add(-time.Minute).UTC().Format(time.RFC3339),
		ListOptions: github.ListOptions{PerPage: 50},
	})
	if err != nil {
		return Run{}, false, err
	}
	for _, r := range runs.WorkflowRuns {
		if strings.Contains(r.GetDisplayTitle(), correlationID) {
			return toRun(r), true, nil
		}
	}
	return Run{}, false, nil
}

// GetRun fetches a run with an If-None-Match conditional request. When GitHub
// answers 304 the run is unchanged (notModified) and does not count against the
// primary rate limit. go-github has no per-call header option, so the request
// is built by hand.
func (c *Client) GetRun(ctx context.Context, runID int64, etag string) (run Run, newETag string, notModified bool, err error) {
	req, err := c.gh.NewRequest(http.MethodGet,
		fmt.Sprintf("repos/%s/%s/actions/runs/%d", c.owner, c.repo, runID), nil)
	if err != nil {
		return Run{}, "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	var wr github.WorkflowRun
	resp, err := c.gh.Do(ctx, req, &wr)
	if statusOf(resp, err) == http.StatusNotModified {
		return Run{}, etag, true, nil
	}
	if err != nil {
		return Run{}, "", false, err
	}
	return toRun(&wr), resp.Header.Get("ETag"), false, nil
}

// Step and Job mirror the Actions job/step timings used for synthetic spans.
type Step struct {
	Name, Conclusion       string
	Number                 int64
	StartedAt, CompletedAt time.Time
}

type Job struct {
	Name, Conclusion       string
	StartedAt, CompletedAt time.Time
	Steps                  []Step
}

// ListJobs returns the run's jobs with step timings (1-second resolution).
func (c *Client) ListJobs(ctx context.Context, runID int64) ([]Job, error) {
	jobs, _, err := c.gh.Actions.ListWorkflowJobs(ctx, c.owner, c.repo, runID,
		&github.ListWorkflowJobsOptions{ListOptions: github.ListOptions{PerPage: 50}})
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(jobs.Jobs))
	for _, j := range jobs.Jobs {
		job := Job{Name: j.GetName(), Conclusion: j.GetConclusion(),
			StartedAt: j.GetStartedAt().Time, CompletedAt: j.GetCompletedAt().Time}
		for _, s := range j.Steps {
			job.Steps = append(job.Steps, Step{Name: s.GetName(), Conclusion: s.GetConclusion(), Number: s.GetNumber(),
				StartedAt: s.GetStartedAt().Time, CompletedAt: s.GetCompletedAt().Time})
		}
		out = append(out, job)
	}
	return out, nil
}
```
### 8.5 Tests

The tests run the real client against the fake GitHub from section 5, through a recording wrapper.

`internal/ghapp/validate_test.go`

```go
package ghapp

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRefInput(t *testing.T) {
	ok := []struct {
		t   RefType
		ref string
	}{
		{RefBranch, "feature/x"}, {RefBranch, "release/1.2"}, {RefBranch, "main"},
		{RefPR, "12"}, {RefPR, "999999999"},
		{RefSHA, "abc1234"}, {RefSHA, strings.Repeat("a", 40)},
	}
	for _, c := range ok {
		if err := ValidateRefInput(c.t, c.ref); err != nil {
			t.Errorf("%s %q: unexpected %v", c.t, c.ref, err)
		}
	}
	bad := []struct {
		t   RefType
		ref string
	}{
		{RefBranch, "../x"}, {RefBranch, "-x"}, {RefBranch, "/x"}, {RefBranch, "a b"},
		{RefBranch, "%2e"}, {RefBranch, strings.Repeat("a", 201)}, {RefBranch, "x;rm"}, {RefBranch, ""},
		{RefBranch, "a..b"},
		{RefPR, "12a"}, {RefPR, "0123456789"}, {RefPR, ""},
		{RefSHA, "XYZ1234"}, {RefSHA, "abc12"}, {RefSHA, strings.Repeat("a", 41)},
		{RefType("tag"), "v1"},
	}
	for _, c := range bad {
		if err := ValidateRefInput(c.t, c.ref); !errors.Is(err, ErrRefInvalid) {
			t.Errorf("%s %q: err = %v, want ErrRefInvalid", c.t, c.ref, err)
		}
	}
}

func TestImageTag(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	for format, want := range map[string]string{
		"{sha}": sha, "{sha7}": "0123456", "sha-{sha7}": "sha-0123456", "v{sha}": "v" + sha,
	} {
		if got := ImageTag(format, sha); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", format, got, want)
		}
	}
}
```
`internal/ghapp/ghapp_test.go`

```go
package ghapp

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/fakegithub"
)

// recorder wraps the fake GitHub, counting requests per path and capturing the
// Authorization header of access_tokens calls (the App JWT).
type recorder struct {
	mu       sync.Mutex
	inner    http.Handler
	paths    map[string]int
	jwts     []string
	requests int
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests++
	r.paths[req.Method+" "+req.URL.Path]++
	if strings.HasSuffix(req.URL.Path, "/access_tokens") {
		r.jwts = append(r.jwts, strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
	}
	r.mu.Unlock()
	r.inner.ServeHTTP(w, req)
}

func (r *recorder) count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paths[key]
}

type fixture struct {
	fake *fakegithub.Server
	rec  *recorder
	srv  *httptest.Server
	cfg  config.Config
	c    *Client
}

func newFixture(t *testing.T, mutate ...func(*config.Config)) *fixture {
	t.Helper()
	fake := fakegithub.New()
	rec := &recorder{inner: fake, paths: map[string]int{}}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		GitHubAPIURL: srv.URL + "/", GHCRBaseURL: srv.URL, ImageTagFormat: "{sha}",
		GitHubAppID: 12345, GitHubAppInstallationID: 777, GitHubAppPrivateKeyFile: "unused",
		EnvironmentsRepo: "ephematool-e2e/ephematool-environments", EnvironmentsWorkflow: "create-environment.yml",
		EnvironmentsRef: "main",
	}
	for _, m := range mutate {
		m(&cfg)
	}
	return &fixture{fake: fake, rec: rec, srv: srv, cfg: cfg, c: NewWithKey(cfg, key)}
}

func TestInstallationTokenIsCached(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 3; i++ {
		if _, err := f.c.IsTeamMember(t.Context(), "ephematool-e2e", "creators", "octo-creator"); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.fake.InstallationTokens(); got != 1 {
		t.Fatalf("minted %d installation tokens for 3 calls, want 1 (cache)", got)
	}
}

func TestTokenRefreshedWhenNearExpiry(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	f.c.now = func() time.Time { return clock }
	_, _ = f.c.IsTeamMember(t.Context(), "ephematool-e2e", "creators", "octo-creator")
	clock = clock.Add(56 * time.Minute) // token lives 1h; < 5 min left means refresh
	_, _ = f.c.IsTeamMember(t.Context(), "ephematool-e2e", "creators", "octo-creator")
	if got := f.fake.InstallationTokens(); got != 2 {
		t.Fatalf("minted %d tokens, want 2 (refresh inside the 5 minute margin)", got)
	}
}

func TestAppJWTShape(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.IsTeamMember(t.Context(), "ephematool-e2e", "creators", "octo-creator"); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(f.rec.jwts[0], ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts", len(parts))
	}
	dec := func(p string, v any) {
		raw, err := base64.RawURLEncoding.DecodeString(p)
		if err != nil || json.Unmarshal(raw, v) != nil {
			t.Fatalf("cannot decode %q", p)
		}
	}
	var hdr struct{ Alg string }
	var cl struct {
		Iss      string
		Iat, Exp int64
	}
	dec(parts[0], &hdr)
	dec(parts[1], &cl)
	now := time.Now().Unix()
	if hdr.Alg != "RS256" || cl.Iss != strconv.Itoa(12345) {
		t.Errorf("header/iss = %v %v", hdr.Alg, cl.Iss)
	}
	if d := now - cl.Iat; d < 55 || d > 65 {
		t.Errorf("iat is %ds in the past, want ~60 (clock skew allowance)", d)
	}
	if d := cl.Exp - now; d < 8*60 || d > 9*60+5 {
		t.Errorf("exp is %ds ahead, want ~9 minutes", d)
	}
}

func TestIsTeamMember(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check := func(login string, want bool) {
		t.Helper()
		got, err := f.c.IsTeamMember(ctx, "ephematool-e2e", "creators", login)
		if err != nil || got != want {
			t.Errorf("%s: member=%v err=%v, want %v", login, got, err, want)
		}
	}
	check("octo-creator", true)
	check("octo-viewer", false)
	check("octo-demoted", true)
	f.fake.SetTeamMembership("ephematool-e2e", "creators", "octo-demoted", "none")
	check("octo-demoted", false) // stale-role closed: no cache between requests
}

func TestNewNotConfigured(t *testing.T) {
	if _, err := New(config.Config{}); err != ErrNotConfigured {
		t.Fatalf("New with empty config: %v, want ErrNotConfigured", err)
	}
}

func TestRetryAfterNonRateLimitError(t *testing.T) {
	if _, ok := RetryAfter(http.ErrAbortHandler); ok {
		t.Fatal("a plain error is not a rate limit")
	}
}
```
`internal/ghapp/refs_test.go`

```go
package ghapp

import (
	"errors"
	"strings"
	"testing"

	"github.com/ephematool/ephematool/internal/config"
)

const demoRepo = "ephematool-e2e/demo-app"

func TestResolveRef(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	mainSHA, err := f.c.ResolveRef(ctx, demoRepo, RefBranch, "main")
	if err != nil || len(mainSHA) != 40 {
		t.Fatalf("main: %q %v", mainSHA, err)
	}
	featSHA, err := f.c.ResolveRef(ctx, demoRepo, RefBranch, "feature/x")
	if err != nil || featSHA == mainSHA {
		t.Fatalf("feature/x: %q %v", featSHA, err)
	}
	if _, err := f.c.ResolveRef(ctx, demoRepo, RefBranch, "missing-x"); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("missing-x: %v, want ErrRefNotFound", err)
	}
	if got, err := f.c.ResolveRef(ctx, demoRepo, RefPR, "1"); err != nil || got != featSHA {
		t.Fatalf("PR 1: %q %v, want the feature/x sha", got, err)
	}
	for _, pr := range []string{"2", "3", "4"} { // fork, deleted fork, closed
		if _, err := f.c.ResolveRef(ctx, demoRepo, RefPR, pr); !errors.Is(err, ErrPRNotEligible) {
			t.Errorf("PR %s: %v, want ErrPRNotEligible", pr, err)
		}
	}
	if got, err := f.c.ResolveRef(ctx, demoRepo, RefSHA, mainSHA[:7]); err != nil || got != mainSHA {
		t.Fatalf("short sha: %q %v, want %q", got, err, mainSHA)
	}
}

func TestResolveRefInvalidInputMakesNoRequests(t *testing.T) {
	f := newFixture(t)
	before := f.rec.requests
	for _, c := range []struct {
		t   RefType
		ref string
	}{{RefBranch, "../x"}, {RefBranch, "x;rm"}, {RefPR, "12a"}, {RefSHA, "XYZ1234"}} {
		if _, err := f.c.ResolveRef(t.Context(), demoRepo, c.t, c.ref); !errors.Is(err, ErrRefInvalid) {
			t.Errorf("%s %q: %v", c.t, c.ref, err)
		}
	}
	if f.rec.requests != before {
		t.Fatalf("invalid input caused %d HTTP requests, want 0", f.rec.requests-before)
	}
}

func TestImageRef(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	sha, _ := f.c.ResolveRef(ctx, demoRepo, RefBranch, "main")

	got, err := f.c.ImageRef(ctx, demoRepo, sha)
	if err != nil || !strings.HasPrefix(got, "ghcr.io/ephematool-e2e/demo-app@sha256:") || len(got) != len("ghcr.io/ephematool-e2e/demo-app@sha256:")+64 {
		t.Fatalf("ImageRef = %q %v", got, err)
	}
	noimg, _ := f.c.ResolveRef(ctx, demoRepo, RefBranch, "noimage-x")
	if _, err := f.c.ImageRef(ctx, demoRepo, noimg); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("noimage: %v, want ErrImageNotFound", err)
	}
	if _, err := f.c.ImageRef(ctx, "somebody/else", sha); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("unknown repo (403): %v, want ErrImageNotFound", err)
	}
	// Mixed-case repo input still resolves: GHCR names are lowercase.
	if _, err := f.c.ImageRef(ctx, "Ephematool-E2E/Demo-App", sha); err != nil {
		t.Fatalf("mixed case repo: %v", err)
	}
}

func TestImageRefSha7Format(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.ImageTagFormat = "{sha7}" })
	sha, _ := f.c.ResolveRef(t.Context(), demoRepo, RefBranch, "main")
	if _, err := f.c.ImageRef(t.Context(), demoRepo, sha); err != nil {
		t.Fatalf("sha7 tag: %v", err)
	}
	if f.rec.count("HEAD /v2/ephematool-e2e/demo-app/manifests/"+sha[:7]) == 0 {
		t.Fatal("the registry was not asked for the 7-char tag")
	}
}
```
`internal/ghapp/actions_test.go`

```go
package ghapp

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func dispatchInput(t *testing.T, f *fixture, envID, branch string) DispatchInput {
	t.Helper()
	sha, err := f.c.ResolveRef(t.Context(), demoRepo, RefBranch, branch)
	if err != nil {
		t.Fatal(err)
	}
	return DispatchInput{
		EnvID: envID, CorrelationID: "corr-" + envID, Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		Repo: demoRepo, SHA: sha, Image: "ghcr.io/ephematool-e2e/demo-app@sha256:" + fmt.Sprintf("%064d", 1),
		Template: "web-app", ChartVersion: "0.1.0", Size: "S", ValuesB64: "e30=", Owner: "octo-admin",
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}
}

func TestDispatchDefaultScenario(t *testing.T) {
	f := newFixture(t)
	res, err := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-aaaaaaaaaa", "main"))
	if err != nil || res.RunID == 0 || res.HTMLURL == "" || res.Ambiguous {
		t.Fatalf("Dispatch = %+v %v, want a run id and no ambiguity", res, err)
	}
	d := f.fake.Dispatches()
	if len(d) != 1 || len(d[0].Inputs) != 12 || d[0].Inputs["env_id"] != "pv-aaaaaaaaaa" || d[0].Inputs["size"] != "S" {
		t.Fatalf("fake recorded %+v, want exactly 12 inputs", d)
	}
}

func TestDispatchAmbiguousThenCorrelate(t *testing.T) {
	f := newFixture(t)
	start := time.Now()
	res, err := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-bbbbbbbbbb", "no-runid-x"))
	if err != nil || !res.Ambiguous || res.RunID != 0 {
		t.Fatalf("Dispatch = %+v %v, want Ambiguous", res, err)
	}
	run, found, err := f.c.FindRunByCorrelation(t.Context(), "corr-pv-bbbbbbbbbb", start)
	if err != nil || !found || run.ID == 0 {
		t.Fatalf("FindRunByCorrelation = %+v %v %v", run, found, err)
	}
	if _, found, _ := f.c.FindRunByCorrelation(t.Context(), "corr-unknown", start); found {
		t.Fatal("unknown correlation id must not match")
	}
}

func TestGetRunETag(t *testing.T) {
	f := newFixture(t)
	res, _ := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-aaaaaaaaaa", "main"))
	run, etag, notModified, err := f.c.GetRun(t.Context(), res.RunID, "")
	if err != nil || notModified || etag == "" || run.Status != "queued" {
		t.Fatalf("first GetRun: %+v %q %v %v", run, etag, notModified, err)
	}
	if _, _, nm, err := f.c.GetRun(t.Context(), res.RunID, etag); err != nil || !nm {
		t.Fatalf("repeat with etag: notModified=%v err=%v, want 304 handled", nm, err)
	}
	f.fake.AdvanceRun(res.RunID)
	run, newTag, nm, err := f.c.GetRun(t.Context(), res.RunID, etag)
	if err != nil || nm || newTag == etag || run.Status != "in_progress" {
		t.Fatalf("after advance: %+v %q %v %v", run, newTag, nm, err)
	}
}

func TestListJobs(t *testing.T) {
	f := newFixture(t)
	res, _ := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-aaaaaaaaaa", "main"))
	f.fake.AdvanceRun(res.RunID)
	f.fake.AdvanceRun(res.RunID)
	jobs, err := f.c.ListJobs(t.Context(), res.RunID)
	if err != nil || len(jobs) != 1 || jobs[0].Name != "provision" || len(jobs[0].Steps) != 3 {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	if jobs[0].StartedAt.IsZero() || jobs[0].Steps[2].CompletedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", jobs[0])
	}
}

func TestDispatchRejectedMapsToSentinel(t *testing.T) {
	f := newFixture(t)
	in := dispatchInput(t, f, "pv-aaaaaaaaaa", "main")
	f.c.extraInputs = map[string]any{}
	for i := 0; i < 14; i++ { // 12 + 14 = 26 > 25
		f.c.extraInputs[fmt.Sprintf("x%d", i)] = "v"
	}
	if _, err := f.c.Dispatch(t.Context(), in); !errors.Is(err, ErrDispatchRejected) {
		t.Fatalf("26 inputs: %v, want ErrDispatchRejected", err)
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/ghapp/...
```

---

## 9. Roles, request-time checks and secure-by-default routes (plan 02-06)

Three review findings land together. **WR-01:** the role used to be frozen in the session at login, so removing someone from a team changed nothing for up to 30 days. Now the role is resolved **on every request** from config and the GitHub team API. **WR-02:** `RequireSession` was opt-in per operation, so a new route that forgot `Security` shipped unauthenticated. Now every operation requires a session unless it carries the `x-public` marker, and a test walks the OpenAPI document to enforce it. **WR-10:** a stale session could keep another identity's keys; the callback now clears the session first.

Roles: **viewer** < **creator** < **admin**. Admin comes from `ADMIN_LOGINS`; creator from `CREATOR_LOGINS` or membership in a `CREATOR_TEAMS` team. A failed team lookup returns viewer (fail closed).

### 9.1 `internal/auth/role.go` (replace)

`RoleFor` is gone. Delete `TestRole` from `internal/auth/allowlist_test.go` (it tested `RoleFor`; `role_test.go` below replaces it):

`internal/auth/allowlist_test.go`

```go

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
`internal/auth/role.go`

```go
package auth

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/alexedwards/scs/v2"

	"github.com/ephematool/ephematool/internal/config"
)

// Roles, lowest to highest. A creator can create environments; an admin can do
// that and read the audit log.
const (
	RoleViewer  = "viewer"
	RoleCreator = "creator"
	RoleAdmin   = "admin"
)

// TeamChecker answers "is login an active member of org/slug?". *ghapp.Client
// satisfies it (auth must not import ghapp).
type TeamChecker interface {
	IsTeamMember(ctx context.Context, org, slug, login string) (bool, error)
}

// Resolver turns a login into a role at request time (WR-01).
type Resolver interface {
	Resolve(ctx context.Context, login string) (string, error)
}

// RoleResolver implements D-06: admin from ADMIN_LOGINS, creator from
// CREATOR_LOGINS or membership of a CREATOR_TEAMS team, otherwise viewer.
// Membership is looked up per call with the platform's App token, so removing
// someone from the team takes effect on their next request, not at next login.
type RoleResolver struct {
	AdminLogins, CreatorLogins, CreatorTeams []string
	Teams                                    TeamChecker // nil: team checks are skipped

	warnOnce *sync.Once // logs the "teams unchecked" warning once per resolver
}

// NewRoleResolver builds a resolver from config. teams may be nil.
func NewRoleResolver(cfg config.Config, teams TeamChecker) RoleResolver {
	return RoleResolver{
		AdminLogins: cfg.AdminLogins, CreatorLogins: cfg.CreatorLogins, CreatorTeams: cfg.CreatorTeams,
		Teams: teams, warnOnce: &sync.Once{},
	}
}

func inList(list []string, login string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), login) {
			return true
		}
	}
	return false
}

// Resolve returns the caller's current role. Order: admin, creator login,
// creator team, viewer. A failed team lookup returns viewer plus the error:
// fail closed, and the caller logs auth.role_check_failed.
func (r RoleResolver) Resolve(ctx context.Context, login string) (string, error) {
	if inList(r.AdminLogins, login) {
		return RoleAdmin, nil
	}
	if inList(r.CreatorLogins, login) {
		return RoleCreator, nil
	}
	if len(r.CreatorTeams) == 0 {
		return RoleViewer, nil
	}
	if r.Teams == nil {
		if r.warnOnce != nil {
			r.warnOnce.Do(func() {
				slog.WarnContext(ctx, "auth.creator_teams_unchecked",
					"reason", "GitHub App not configured; CREATOR_TEAMS cannot be evaluated")
			})
		}
		return RoleViewer, nil
	}
	for _, team := range r.CreatorTeams {
		org, slug, ok := strings.Cut(team, "/")
		if !ok {
			continue
		}
		member, err := r.Teams.IsTeamMember(ctx, org, slug, login)
		if err != nil {
			return RoleViewer, err
		}
		if member {
			return RoleCreator, nil
		}
	}
	return RoleViewer, nil
}

var rank = map[string]int{RoleViewer: 1, RoleCreator: 2, RoleAdmin: 3}

// RoleAtLeast reports whether role is min or higher (viewer < creator < admin).
// Unknown roles rank 0 and never satisfy anything.
func RoleAtLeast(role, min string) bool { return rank[role] >= rank[min] && rank[role] > 0 }

// Principal is the signed-in user as stored in the session.
type Principal struct {
	GithubID  int64
	Login     string
	AvatarURL string
}

// PrincipalFrom reads the signed-in user from the session; ok is false when
// nobody is signed in.
func PrincipalFrom(ctx context.Context, sm *scs.SessionManager) (Principal, bool) {
	if sm == nil {
		return Principal{}, false
	}
	id := sm.GetInt64(ctx, KeyUserID)
	if id == 0 {
		return Principal{}, false
	}
	return Principal{GithubID: id, Login: sm.GetString(ctx, KeyLogin), AvatarURL: sm.GetString(ctx, KeyAvatarURL)}, true
}

// userTeamChecker adapts the login-time, user-token MembershipClient so the
// login hint can see creator teams even before the App is configured.
type userTeamChecker struct{ mc MembershipClient }

func (u userTeamChecker) IsTeamMember(ctx context.Context, org, slug, login string) (bool, error) {
	state, status, err := u.mc.TeamMembership(ctx, org, slug, login)
	if status == 404 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == 200 && state == stateActive, nil
}
```
### 9.2 `internal/auth/middleware.go` (replace)

`RequireRole` is a per-operation middleware. It takes a `Resolver` **interface**, so tests (and the audit API in 2d) can swap the answer between two requests and prove that a demotion applies immediately. The 403 body is the same generic "access denied" whatever failed.

`internal/auth/middleware.go`

```go
package auth

import (
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
)

// PublicExtension is the OpenAPI extension that opts an operation out of the
// session requirement. Everything else is protected by default (WR-02).
const PublicExtension = "x-public"

// MarkPublic exempts an operation from RequireSession. Use it sparingly; the
// httpapi security test lists every public operation.
func MarkPublic(op *huma.Operation) {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[PublicExtension] = true
}

func isPublic(op *huma.Operation) bool {
	v, _ := op.Extensions[PublicExtension].(bool)
	return v
}

// RequireSession is secure by default: every operation answers 401 without a
// signed-in session unless it is marked public. A handler that forgets
// Security can no longer ship unauthenticated.
func RequireSession(api huma.API, sm *scs.SessionManager) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op != nil && isPublic(op) {
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

// RequireRole is a per-operation middleware (huma.Operation.Middlewares). It
// resolves the caller's role NOW, not from the session, so a demotion applies
// to the very next request. Below min, or on a lookup failure, it answers 403
// with the generic "access denied" (no detail about which check failed).
func RequireRole(api huma.API, sm *scs.SessionManager, r Resolver, min string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		p, ok := PrincipalFrom(ctx.Context(), sm)
		if !ok {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		role, err := r.Resolve(ctx.Context(), p.Login)
		if err != nil {
			slog.WarnContext(ctx.Context(), "auth.role_check_failed", "login", p.Login, "error", err.Error())
		}
		if err != nil || !RoleAtLeast(role, min) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "access denied")
			return
		}
		next(ctx)
	}
}
```
### 9.3 `internal/auth/handlers.go` (replace)

Changes from Phase 1: a `Roles` field; the login stores a role **hint** only; `Clear()` before `RenewToken` (WR-10); `/api/me` resolves the role live; `get-auth-config` and `post-auth-logout` carry `x-public`; the role enum gains `creator`.

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

	// Roles resolves a login to a role. cmd/api sets it with the GitHub App
	// client as its TeamChecker; the default has no team checker.
	Roles RoleResolver
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
		Roles:  NewRoleResolver(cfg, nil),
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
	// The role stored in the session is only a login-time hint for the UI. The
	// authoritative role is re-resolved on every request (RequireRole, /api/me).
	// If no App token checker is configured, fall back to the user's own token
	// so team-based creators still see the right hint.
	roles := h.Roles
	if roles.Teams == nil {
		roles.Teams = userTeamChecker{mc: GitHubMembershipClient{Client: gh}}
	}
	role, rerr := roles.Resolve(ctx, login)
	if rerr != nil {
		slog.WarnContext(ctx, "auth.role_check_failed", "login", login, "error", rerr.Error())
	}
	// WR-10: drop everything from any previous identity before issuing the new
	// session, so no stale key can survive an account switch in the same browser.
	if err := h.sm.Clear(ctx); err != nil {
		h.fail(w, r, "session_clear")
		return
	}
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
	Role      string   `json:"role" enum:"admin,creator,viewer"`
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

// currentRole is the caller's role right now. With the App configured it is
// resolved live (a demotion shows on the next page load). Without it, the
// login-time hint is kept, but never below what the config alone proves.
func (h *Handlers) currentRole(ctx context.Context, login string) string {
	role, err := h.Roles.Resolve(ctx, login)
	if err != nil {
		slog.WarnContext(ctx, "auth.role_check_failed", "login", login, "error", err.Error())
		return RoleViewer // fail closed
	}
	if h.Roles.Teams == nil {
		if hint := h.sm.GetString(ctx, KeyRole); RoleAtLeast(hint, role) {
			return hint
		}
	}
	return role
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
		login, avatar := h.sm.GetString(ctx, KeyLogin), h.sm.GetString(ctx, KeyAvatarURL)
		if h.q != nil { // read the persisted user (first real DB read)
			if u, err := h.q.GetUserByGithubID(ctx, id); err == nil {
				login, avatar = u.Login, u.AvatarUrl
			}
		}
		return &MeResponse{Body: MeBody{
			Login: login, AvatarURL: avatar, Role: h.currentRole(ctx, login), Orgs: nonNil(orgs),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-config", Method: http.MethodGet, Path: "/api/auth/config",
		Summary:    "Public sign-in configuration",
		Extensions: map[string]any{PublicExtension: true}, // login page needs it before sign-in
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
		Extensions: map[string]any{PublicExtension: true}, // idempotent: logging out twice must not 401
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
### 9.4 Tests

`internal/auth/role_test.go`

```go
package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/config"
)

type fakeTeams struct {
	mu      sync.Mutex
	members map[string]bool // "org/slug/login" -> member
	err     error
}

func (f *fakeTeams) IsTeamMember(_ context.Context, org, slug, login string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.members[org+"/"+slug+"/"+login], f.err
}

func (f *fakeTeams) set(key string, v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members[key] = v
}

func TestResolve(t *testing.T) {
	teams := &fakeTeams{members: map[string]bool{"org/creators/octo-team": true}}
	cfg := config.Config{
		AdminLogins: []string{"Octo-Admin"}, CreatorLogins: []string{"octo-direct"}, CreatorTeams: []string{"org/creators"},
	}
	r := NewRoleResolver(cfg, teams)
	cases := map[string]string{
		"octo-admin":  RoleAdmin,   // case-insensitive admin
		"octo-direct": RoleCreator, // CREATOR_LOGINS
		"octo-team":   RoleCreator, // team member
		"octo-nobody": RoleViewer,
	}
	for login, want := range cases {
		if got, err := r.Resolve(t.Context(), login); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", login, got, err, want)
		}
	}
}

func TestResolveAdminBeatsCreator(t *testing.T) {
	cfg := config.Config{AdminLogins: []string{"x"}, CreatorLogins: []string{"x"}}
	if got, _ := NewRoleResolver(cfg, nil).Resolve(t.Context(), "x"); got != RoleAdmin {
		t.Fatalf("got %q, want admin", got)
	}
}

func TestResolveFailsClosed(t *testing.T) {
	boom := errors.New("github down")
	r := NewRoleResolver(config.Config{CreatorTeams: []string{"org/creators"}}, &fakeTeams{err: boom})
	got, err := r.Resolve(t.Context(), "someone")
	if got != RoleViewer || !errors.Is(err, boom) {
		t.Fatalf("got %q %v, want viewer plus the error", got, err)
	}
}

func TestResolveWithoutChecker(t *testing.T) {
	r := NewRoleResolver(config.Config{CreatorTeams: []string{"org/creators"}}, nil)
	if got, err := r.Resolve(t.Context(), "team-only"); err != nil || got != RoleViewer {
		t.Fatalf("got %q %v, want viewer and no error", got, err)
	}
}

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		role, min string
		want      bool
	}{
		{RoleViewer, RoleCreator, false}, {RoleCreator, RoleCreator, true}, {RoleAdmin, RoleCreator, true},
		{RoleCreator, RoleAdmin, false}, {RoleViewer, RoleViewer, true}, {"", RoleViewer, false}, {"root", RoleViewer, false},
	}
	for _, c := range cases {
		if got := RoleAtLeast(c.role, c.min); got != c.want {
			t.Errorf("RoleAtLeast(%q,%q) = %v, want %v", c.role, c.min, got, c.want)
		}
	}
}

// stubResolver lets a test change the answer between two requests.
type stubResolver struct {
	mu   sync.Mutex
	role string
	err  error
}

func (s *stubResolver) Resolve(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.role, s.err
}

func (s *stubResolver) set(role string) { s.mu.Lock(); s.role = role; s.mu.Unlock() }

type roleHarness struct {
	h      http.Handler
	cookie *http.Cookie
}

func newRoleHarness(t *testing.T, res Resolver) *roleHarness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	sm := NewSessionManager(rdb, config.Config{SessionIdleTimeout: time.Hour, SessionLifetime: 2 * time.Hour})

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(RequireSession(api, sm))
	mux.HandleFunc("POST /test/login", func(w http.ResponseWriter, r *http.Request) {
		_ = sm.RenewToken(r.Context())
		sm.Put(r.Context(), KeyUserID, int64(42))
		sm.Put(r.Context(), KeyLogin, "octo")
	})
	ok := func(context.Context, *struct{}) (*struct{}, error) { return nil, nil }
	huma.Register(api, huma.Operation{OperationID: "admin-only", Method: http.MethodGet, Path: "/admin",
		Middlewares: huma.Middlewares{RequireRole(api, sm, res, RoleAdmin)}, DefaultStatus: 204}, ok)
	huma.Register(api, huma.Operation{OperationID: "unmarked", Method: http.MethodGet, Path: "/unmarked", DefaultStatus: 204}, ok)
	pub := huma.Operation{OperationID: "public", Method: http.MethodGet, Path: "/public", DefaultStatus: 204}
	MarkPublic(&pub)
	huma.Register(api, pub, ok)

	hh := sm.LoadAndSave(mux)
	rec := httptest.NewRecorder()
	hh.ServeHTTP(rec, httptest.NewRequest("POST", "/test/login", nil))
	var c *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == CookieName {
			c = ck
		}
	}
	if c == nil {
		t.Fatal("login set no session cookie")
	}
	return &roleHarness{h: hh, cookie: c}
}

func (r *roleHarness) get(path string, withSession bool) int {
	req := httptest.NewRequest("GET", path, nil)
	if withSession {
		req.AddCookie(r.cookie)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRequireRoleResolvesPerRequest(t *testing.T) {
	res := &stubResolver{role: RoleCreator}
	h := newRoleHarness(t, res)
	if got := h.get("/admin", true); got != 403 {
		t.Fatalf("creator on admin route: %d, want 403", got)
	}
	res.set(RoleAdmin) // promoted without a new login
	if got := h.get("/admin", true); got != 204 {
		t.Fatalf("after promotion: %d, want 204", got)
	}
	res.set(RoleViewer) // demoted without a new login
	if got := h.get("/admin", true); got != 403 {
		t.Fatalf("after demotion: %d, want 403 (stale-role closed)", got)
	}
}

func TestRequireRoleLookupErrorIs403(t *testing.T) {
	h := newRoleHarness(t, &stubResolver{role: RoleAdmin, err: errors.New("boom")})
	if got := h.get("/admin", true); got != 403 {
		t.Fatalf("resolver error: %d, want 403 (fail closed)", got)
	}
}

func TestSecureByDefault(t *testing.T) {
	h := newRoleHarness(t, &stubResolver{role: RoleAdmin})
	if got := h.get("/unmarked", false); got != 401 {
		t.Fatalf("operation with no Security and no x-public, no session: %d, want 401", got)
	}
	if got := h.get("/unmarked", true); got != 204 {
		t.Fatalf("same operation with a session: %d, want 204", got)
	}
	if got := h.get("/public", false); got != 204 {
		t.Fatalf("x-public operation without a session: %d, want 204", got)
	}
	if got := h.get("/admin", false); got != 401 {
		t.Fatalf("admin route without a session: %d, want 401", got)
	}
}
```
`internal/auth/role_integration_test.go`

```go
//go:build integration

package auth_test

import (
	"testing"

	"github.com/ephematool/ephematool/internal/auth/authtest"
	"github.com/ephematool/ephematool/internal/config"
)

// A user in the creators team signs in and is a creator (the role hint comes
// from the user's own token because no GitHub App checker is configured here).
func TestCreatorTeamRole(t *testing.T) {
	e := authtest.NewEnv(t, func(c *config.Config) {
		c.CreatorTeams = []string{"ephematool-e2e/creators"}
	})
	e.Login(t, "octo-creator", "/")
	if status, me := getMe(t, e); status != 200 || me.Role != "creator" || me.Login != "octo-creator" {
		t.Fatalf("me = %d %+v, want creator", status, me)
	}
}

// WR-10: switching accounts in one browser must not leak the previous identity.
func TestAccountSwitchLeavesNoAdminData(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Login(t, "octo-admin", "/")
	if _, me := getMe(t, e); me.Role != "admin" {
		t.Fatalf("setup: first login should be admin, got %+v", me)
	}
	e.Login(t, "octo-viewer", "/") // same cookie jar
	status, me := getMe(t, e)
	if status != 200 || me.Role != "viewer" || me.Login != "octo-viewer" {
		t.Fatalf("after switching accounts me = %d %+v, want viewer octo-viewer", status, me)
	}
}
```
`internal/httpapi/security_test.go`

```go
package httpapi

import (
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ephematool/ephematool/internal/auth"
)

// operations lists every operation of an OpenAPI document by operation id.
func operations(oa *huma.OpenAPI) map[string]*huma.Operation {
	out := map[string]*huma.Operation{}
	for _, item := range oa.Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace} {
			if op != nil {
				out[op.OperationID] = op
			}
		}
	}
	return out
}

// Every operation must either require the session scheme or be explicitly
// public. New routes therefore cannot silently ship unauthenticated (WR-02).
// Note: New is called with nil deps, so every register function must be nil-safe.
func TestEveryOperationIsProtectedOrPublic(t *testing.T) {
	_, api := New(Deps{})
	var public []string
	for id, op := range operations(api.OpenAPI()) {
		isPublic, _ := op.Extensions[auth.PublicExtension].(bool)
		hasSession := false
		for _, req := range op.Security {
			if _, ok := req["session"]; ok {
				hasSession = true
			}
		}
		if !hasSession && !isPublic {
			t.Errorf("operation %q has neither session Security nor x-public", id)
		}
		if isPublic {
			public = append(public, id)
		}
	}
	sort.Strings(public)
	want := []string{"get-auth-config", "post-auth-logout"}
	if len(public) != len(want) || public[0] != want[0] || public[1] != want[1] {
		t.Fatalf("public operations = %v, want exactly %v", public, want)
	}
}
```
> `security_test.go` calls `httpapi.New(httpapi.Deps{})` with **nil** dependencies, exactly as `go generate` does to write `api/openapi.json`. Every register function you add in later phases must be nil-safe at registration time, or this test (and OpenAPI generation) will panic.

```bash
bazel run //:gazelle
bazel test //internal/auth/... //internal/httpapi/...
bazel test --config=integration //internal/auth:auth_test
```

### 9.5 Show the new role in the dashboard

Regenerate the API types (the role enum gained `creator`), then replace `RoleBadge`. Its prop type is derived from the generated schema, so a role added on the server becomes a compile error here until a label exists:

`web/src/components/RoleBadge.tsx`

```tsx
import { Badge } from "@/components/ui/badge";
import type { components } from "@/api/schema";

// The role type comes from the generated API schema, so adding a role on the
// server is a compile error here until the label is added.
export type Role = components["schemas"]["MeBody"]["role"];

const LABEL: Record<Role, string> = {
  admin: "Admin",
  creator: "Creator",
  viewer: "Viewer",
};

export function RoleBadge({ role }: { role: Role }) {
  return (
    <Badge variant="secondary" className="text-xs font-semibold">
      {LABEL[role]}
    </Badge>
  );
}
```
`web/src/components/RoleBadge.test.tsx`

```tsx
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { RoleBadge } from "./RoleBadge";

afterEach(cleanup);

describe("RoleBadge", () => {
  it.each([
    ["admin", "Admin"],
    ["creator", "Creator"],
    ["viewer", "Viewer"],
  ] as const)("renders %s as %s", (role, label) => {
    render(<RoleBadge role={role} />);
    expect(screen.getByText(label)).toBeTruthy();
  });
});
```
```bash
bazel run //:gen                          # api/openapi.json and web/src/api/schema.d.ts
bazel test //web:test //:check_gen
```

---

## 10. Run it

```bash
bazel test //...                                   # unit tests across the repo
bazel test --config=integration //:test_integration
bazel run //:up_fake                               # the Phase 1 fake stack must still come up healthy
bazel run //:verify                                # or: bash tools/verify.sh
bazel run //:down
```

---

## 11. Verification status of this document's code

Be clear about what has and has not been executed:

| Part | Status |
|---|---|
| `internal/fakegithub` (all files and tests) | Compiled and run with `go test -race` in a stdlib-only module: pass. |
| `internal/config` (config, validate, tests) | Compiled and run against a small stand-in for `caarlos0/env`: pass. The real library was not available offline. |
| `internal/audit` cursor, limit and trace-id helpers; `internal/ghapp/validate.go` | Compiled and run standalone: pass. |
| SQL migrations, grants, triggers, CAS and paging queries | Executed against PostgreSQL 16 with `psql`: behave as described. (Not goose, not sqlc, not Postgres 18.) |
| `deploy/compose.yaml` api/worker blocks | Validated with `docker compose config`, including the e2e overlay. |
| Widened `conventions.sh` | Ran against the current repository tree: PASS. |
| Everything that imports go-github, jwt, huma, pgx, scs, asynq, testcontainers (`ghapp`, `auth`, `audit`, `store.go`, `testenv`, `cmd/api`) | **Written to the plan and syntax-checked with `gofmt`, but not compiled.** Third-party modules could not be downloaded where this was written. Expect to fix small compile errors (an import, a generated field name) on first build. |

---

## 12. Acceptance checklist

- [ ] `docs/decisions/ADR-0006-phase-2-dependencies.md` exists; you ran the section 2.2 commands and the values match.
- [ ] `bazel test //internal/config/...` passes; every rejection test names the variable.
- [ ] `docker compose -f deploy/compose.yaml config` shows `DATABASE_URL` with `ephematool_app` for api **and** worker, and `MIGRATE_DATABASE_URL` only on api.
- [ ] `bazel test //:check_gen` passes and `bazel query 'tests(//:check_gen)' | grep environments` lists `//environments:gen_tests`.
- [ ] `bash scripts/verify.d/conventions.sh` prints PASS.
- [ ] `bazel test //internal/fakegithub/...` passes, including the Phase 1 PKCE and membership tests.
- [ ] `bazel test --config=integration //internal/store:store_test` passes all five tests.
- [ ] `grep -rn "alter default privileges" -i internal/store/migrations/` prints nothing.
- [ ] `grep -n WithSessionLocker internal/store/store.go` matches; `grep -n "goose.UpContext\|SetBaseFS" internal/store/store.go` prints nothing.
- [ ] `bazel test //internal/audit/... //internal/ghapp/... //internal/auth/... //internal/httpapi/...` passes.
- [ ] `bazel test --config=integration //:test_integration` passes (auth, store, audit).
- [ ] An operation registered with no `Security` and no `x-public` answers 401 without a session (`TestSecureByDefault`).
- [ ] `bazel run //:up_fake` still reaches healthy, and `GET /api/me` reports your role (`viewer`, `creator` or `admin`).

---

## 13. Common failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `ERROR: role "ephematool_app" does not exist` when the API starts | the migration did not run, or an old Postgres volume skipped it | `bazel run //:clean && bazel run //:up_fake`; migration 00002 creates the role |
| API crash-loops with `permission denied for table goose_db_version` | `MIGRATE_DATABASE_URL` is missing or equals the app-role URL | the migrate connection must be the owner (`ephematool`); check `docker compose config` |
| `store_test` skipped | Docker not reachable from the test | use `bazel test --config=integration` (it runs outside the sandbox) and start Docker |
| `bazel test //:check_gen` fails after editing `conventions/telemetry.json` | generated files are stale | `bazel run //:gen`, commit the result |
| `scripts/verify.d/conventions.sh` fails on a YAML/shell file | a hand-written `ephematool.io/` or `ephematool.env.` literal | read the key from `environments/conventions.gen.env` instead |
| `go: github.com/golang-jwt/jwt/v5 ... missing` | module not added | `go get github.com/golang-jwt/jwt/v5@v5.3.1 && bazel mod tidy && bazel run //:gazelle` |
| compile error naming `store.MarkProvisioningParams` fields | sqlc named a field differently than this doc | open `internal/store/models.go` / `environments.sql.go` and use the generated names |
| `TestEveryOperationIsProtectedOrPublic` panics | a register function dereferences a nil dependency | make registration nil-safe; only the handler body may use the dependency |
| `/api/me` says `viewer` for a team creator | App not configured, or the App lacks Organization members read | set the three `GITHUB_APP_*` values; check the App's permissions; the API logs `auth.role_check_failed` |
| Playwright e2e later: `403` for `octo-creator` | `CREATOR_TEAMS` not set in the e2e overlay | added in 2b (`compose.e2e.yaml`) |
