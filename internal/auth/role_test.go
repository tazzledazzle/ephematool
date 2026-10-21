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
