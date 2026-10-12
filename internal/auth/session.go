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
	KeyLastTouch = "last_touch"
)

// NewSessionManager builds the Redis-backed session manager. IdleTimeout (D-03)
// and Lifetime (D-14) are set explicitly because scs defaults to a 24h
// absolute lifetime (Pitfall 1). Sliding renewal lands in plan 01-07.
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
