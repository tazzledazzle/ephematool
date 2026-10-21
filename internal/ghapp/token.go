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
