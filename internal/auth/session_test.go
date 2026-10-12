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
