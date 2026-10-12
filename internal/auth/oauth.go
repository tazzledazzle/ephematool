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
