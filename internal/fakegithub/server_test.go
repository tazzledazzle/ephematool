package fakegithub

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func nonFollowing() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// authorizeCode drives authorize for a login and returns the issued code.
func authorizeCode(t *testing.T, base, login, challenge string) string {
	t.Helper()
	q := url.Values{
		"client_id": {ClientID}, "redirect_uri": {"http://app.test/cb"}, "state": {"s"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "login": {login},
	}
	resp, err := nonFollowing().Get(base + "/login/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if loc.Query().Get("state") != "s" {
		t.Fatalf("state not echoed: %v", loc)
	}
	return loc.Query().Get("code")
}

func exchange(t *testing.T, base, code, verifier string) *http.Response {
	t.Helper()
	form := url.Values{
		"client_id": {ClientID}, "client_secret": {ClientSecret}, "code": {code}, "code_verifier": {verifier},
		"grant_type": {"authorization_code"},
	}
	resp, err := http.Post(base+"/login/oauth/access_token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPKCE(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	verifier := "verifier-verifier-verifier-verifier-verifier-1234"

	code := authorizeCode(t, ts.URL, "octo-admin", s256(verifier))
	bad := exchange(t, ts.URL, code, "wrong-verifier")
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong verifier: got %d, want 400", bad.StatusCode)
	}

	code = authorizeCode(t, ts.URL, "octo-admin", s256(verifier))
	ok := exchange(t, ts.URL, code, verifier)
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("correct verifier: got %d, want 200", ok.StatusCode)
	}
}

func membership(t *testing.T, base, login, path string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer fake-token-"+login)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	b := make([]byte, 512)
	n, _ := resp.Body.Read(b)
	buf.Write(b[:n])
	return resp.StatusCode, buf.String()
}

func TestMemberships(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	org := "/user/memberships/orgs/ephematool-e2e"
	cases := []struct {
		login, path string
		status      int
		state       string
	}{
		{"octo-admin", org, 200, `"active"`},
		{"octo-pending", org, 200, `"pending"`},
		{"octo-outsider", org, 404, ""},
		{"octo-forbidden", org, 403, ""},
		{"octo-teamonly", "/orgs/partner-org/teams/sre/memberships/octo-teamonly", 200, `"active"`},
	}
	for _, c := range cases {
		status, body := membership(t, ts.URL, c.login, c.path)
		if status != c.status || (c.state != "" && !strings.Contains(body, c.state)) {
			t.Errorf("%s %s: got %d %s", c.login, c.path, status, body)
		}
	}
}
