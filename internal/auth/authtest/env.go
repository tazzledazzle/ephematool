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
		PublicBaseURL:        "http://" + srv.Listener.Addr().String(),
		DatabaseURL:          shared.pgURL,
		GitHubClientID:       fakegithub.ClientID,
		GitHubClientSecret:   fakegithub.ClientSecret,
		GitHubAuthorizeURL:   fakeSrv.URL + "/login/oauth/authorize",
		GitHubTokenURL:       fakeSrv.URL + "/login/oauth/access_token",
		GitHubAPIURL:         fakeSrv.URL + "/",
		AllowedOrgs:          []string{fakegithub.Org},
		AllowedTeams:         []string{fakegithub.Team},
		AdminLogins:          []string{"Octo-Admin"},
		SessionIdleTimeout:   168 * time.Hour,
		SessionLifetime:      720 * time.Hour,
		SessionTouchInterval: 5 * time.Minute,
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
