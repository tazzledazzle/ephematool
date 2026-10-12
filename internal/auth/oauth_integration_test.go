//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/ephematool/ephematool/internal/auth/authtest"
	"github.com/ephematool/ephematool/internal/config"
)

type meBody struct {
	Login     string   `json:"login"`
	AvatarURL string   `json:"avatarUrl"`
	Role      string   `json:"role"`
	Orgs      []string `json:"orgs"`
}

func getMe(t *testing.T, e *authtest.Env) (int, meBody) {
	t.Helper()
	resp := e.Get(t, "/api/me")
	var b meBody
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, b
}

func TestOAuthAllowedMember(t *testing.T) {
	e := authtest.NewEnv(t)
	resp := e.Login(t, "octo-admin", "/")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "ephematool_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 && cookie.Expires.IsZero() {
		t.Fatalf("session cookie wrong: %+v", cookie)
	}
	status, me := getMe(t, e)
	if status != 200 || me.Role != "admin" || me.Login != "octo-admin" {
		t.Fatalf("me: %d %+v", status, me)
	}
	u, err := e.Queries.GetUserByGithubID(t.Context(), 1001)
	if err != nil || u.Login != "octo-admin" {
		t.Fatalf("users row: %+v %v", u, err)
	}
	if len(e.Logs.Find("auth.login")) != 1 {
		t.Fatal("expected one auth.login event")
	}
}

func TestOAuthViewerRole(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Login(t, "octo-viewer", "/")
	if status, me := getMe(t, e); status != 200 || me.Role != "viewer" {
		t.Fatalf("me: %d %+v", status, me)
	}
}

func TestOAuthTeamMember(t *testing.T) {
	e := authtest.NewEnv(t)
	resp := e.Login(t, "octo-teamonly", "/")
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("location %q", resp.Header.Get("Location"))
	}
	status, me := getMe(t, e)
	if status != 200 || len(me.Orgs) != 1 || me.Orgs[0] != "partner-org/sre" {
		t.Fatalf("me: %d %+v", status, me)
	}
}

func TestOAuthDeniedNonMember(t *testing.T) {
	for _, login := range []string{"octo-outsider", "octo-pending", "octo-forbidden"} {
		t.Run(login, func(t *testing.T) {
			e := authtest.NewEnv(t)
			resp := e.Login(t, login, "/")
			want := "/denied?reason=org_membership&login=" + url.QueryEscape(login)
			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
				t.Fatalf("callback: %d %q, want %q", resp.StatusCode, resp.Header.Get("Location"), want)
			}
			if e.HasSessionCookie() {
				t.Fatal("denied login must not get a session cookie")
			}
			if len(e.Logs.Find("auth.denied")) != 1 {
				t.Fatal("expected auth.denied event")
			}
			if login == "octo-forbidden" && len(e.Logs.Find("auth.org_check_forbidden")) == 0 {
				t.Fatal("expected auth.org_check_forbidden event")
			}
			if status, _ := getMe(t, e); status != 401 {
				t.Fatalf("me: %d, want 401", status)
			}
		})
	}
}

func TestOAuthStateMismatch(t *testing.T) {
	e := authtest.NewEnv(t)
	cb := e.Authorize(t, e.StartLogin(t, "/"), "octo-admin")
	u, _ := url.Parse(cb)
	q := u.Query()
	q.Set("state", "tampered")
	u.RawQuery = q.Encode()
	resp := e.Get(t, u.String())
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?reason=failed" {
		t.Fatalf("callback: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if e.HasSessionCookie() {
		t.Fatal("no session expected")
	}
	if status, _ := getMe(t, e); status != 401 {
		t.Fatalf("me: %d", status)
	}
}

func TestOAuthPKCESent(t *testing.T) {
	e := authtest.NewEnv(t)
	e.StartLogin(t, "/")
	q := e.Fake.LastAuthorize()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("PKCE params missing: %v", q)
	}
	if q.Get("scope") != "read:org" {
		t.Fatalf("scope %q, want read:org", q.Get("scope"))
	}
}

func TestOAuthOpenRedirect(t *testing.T) {
	for _, next := range []string{"//evil.example", "https://evil.example", `/\evil`} {
		t.Run(next, func(t *testing.T) {
			e := authtest.NewEnv(t)
			resp := e.Login(t, "octo-viewer", next)
			if resp.Header.Get("Location") != "/" {
				t.Fatalf("next %q landed on %q", next, resp.Header.Get("Location"))
			}
		})
	}
}

func TestOAuthNextPreserved(t *testing.T) {
	e := authtest.NewEnv(t)
	resp := e.Login(t, "octo-viewer", "/status?x=1")
	if resp.Header.Get("Location") != "/status?x=1" {
		t.Fatalf("location %q", resp.Header.Get("Location"))
	}
}

func TestOAuthCancelled(t *testing.T) {
	e := authtest.NewEnv(t)
	u, _ := url.Parse(e.StartLogin(t, "/"))
	q := u.Query()
	q.Set("cancel", "1")
	u.RawQuery = q.Encode()
	// Reuse the helper path: authorize with cancel returns the error callback.
	resp, err := http.DefaultTransport.RoundTrip(mustReq(t, u.String()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cb := e.Get(t, resp.Header.Get("Location"))
	if cb.StatusCode != http.StatusFound || cb.Header.Get("Location") != "/login?reason=failed" {
		t.Fatalf("callback: %d %q", cb.StatusCode, cb.Header.Get("Location"))
	}
}

func mustReq(t *testing.T, target string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOAuthNotConfigured(t *testing.T) {
	e := authtest.NewEnv(t, func(c *config.Config) { c.GitHubClientID = ""; c.GitHubClientSecret = "" })
	resp := e.Get(t, "/api/auth/config")
	var body struct {
		GitHubConfigured bool     `json:"githubConfigured"`
		AllowedOrgs      []string `json:"allowedOrgs"`
		AllowedTeams     []string `json:"allowedTeams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.GitHubConfigured {
		t.Fatal("githubConfigured must be false")
	}
	login := e.Get(t, "/api/auth/github/login")
	if login.StatusCode != http.StatusFound || login.Header.Get("Location") != "/login?reason=failed" {
		t.Fatalf("login: %d %q", login.StatusCode, login.Header.Get("Location"))
	}
	if len(e.Logs.Find("auth.github_not_configured")) == 0 {
		t.Fatal("expected auth.github_not_configured event")
	}
}

func TestLogout(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Login(t, "octo-viewer", "/")
	if n := len(e.Redis.Keys(t.Context(), "scs:session:*").Val()); n != 1 {
		t.Fatalf("expected 1 session key, got %d", n)
	}
	if resp := e.Post(t, "/api/auth/logout"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if status, _ := getMe(t, e); status != 401 {
		t.Fatalf("me after logout: %d", status)
	}
	if n := len(e.Redis.Keys(t.Context(), "scs:session:*").Val()); n != 0 {
		t.Fatalf("session key not removed: %d", n)
	}
}
