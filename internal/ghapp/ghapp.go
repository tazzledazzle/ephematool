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
