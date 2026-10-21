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
