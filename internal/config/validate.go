package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type durationCheck struct {
	envName string
	value   time.Duration
}

var (
	orgSlugRe = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`) //org/team and org/repo
	domainRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
)

// heartbeatMax is half the 45ss heartbeat TTL in internal/queue
const heartbeatMax = 22500 * time.Millisecond

// validate returns the first violated rule, naming the env variable.
func validate(c Config) error {

	checks := []durationCheck{
		{"SESSION_IDLE_TIMEOUT", c.SessionIdleTimeout},
		{"SESSION_LIFETIME", c.SessionLifetime},
		{"HEARTBEAT_INTERVAL", c.HeartbeatInterval},
		{"PROBE_INTERVAL", c.ProbeInterval},
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
	// Rule group 1: durations must be positive (PING_THROTTLE may be 0).
	// TODO

	// Rule group 2: relationships between durations
	// TODO
	switch {
	case c.SessionIdleTimeout > c.SessionLifetime:
		return fmt.Errorf("SESSION_IDLE_TIMEOUT must be <= SESSION_LIFETIME")
	case c.ProbeTimeout >= c.ProbeInterval:
		return fmt.Errorf("PROBE_TIMEOUT must be < PROBE_INTERVAL")
	case c.HeartbeatInterval >= heartbeatMax:
		return fmt.Errorf("HEARTBEAT_INTERVAL must be < %s (half the heartbeat TTL)", heartbeatMax)
	case c.EnvPollInitial > c.EnvPollMax:
		return fmt.Errorf("ENV_POLL_INITIAL must be <= ENV_POLL_MAX")
	case c.EnvProbeInitial >= c.EnvProbeWindow:
		return fmt.Errorf("ENV_PROBE_INTERVAL must be < ENV_PROBE_WINDOW")
	case c.EnvProbeTimeout >= c.EnvProbeInitial:
		return fmt.Errorf("ENV_PROBE_TIMEOUT must be < ENV_PROBE_INITIAL")
	case c.PreviewTicketTTL > 5*time.Minute:
		return fmt.Errorf("PREVIEW_TICKET_TTL must be <= 5m")
	}
	// Rule group 3: formats (teams, repos, domain, tag format, scheme, port).
	// TODO
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
	// Rule group 4: WR-03, HTTPS requires COOKIE_SECURE.
	// TODO
	if u, err := url.Parse(c.PublicBaseURL); err == nil && u.Scheme == "https" && !c.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must be true when PUBLIC_BASE_URL is https")
	}
	if c.PreviewScheme == "https" && !c.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must be true when PREVIEW_SCHEME is https")
	}

	for _, ch := range checks {
		if ch.value <= 0 {
			return fmt.Errorf("%s must be > 0", ch.envName)
		}
	}
	return nil
}
