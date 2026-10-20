package fakegithub

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// appJWT builds a structurally valid JWT by hand (base64url JSON), so this
// package stays stdlib-only. alg and the claims are what the fake validates.
func appJWT(t *testing.T, alg string, iss string, exp time.Time, parts int) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	hdr := enc(map[string]string{"alg": alg, "typ": "JWT"})
	claims := enc(map[string]any{"iss": iss, "iat": time.Now().Add(-60 * time.Second).Unix(), "exp": exp.Unix()})
	if parts == 2 {
		return hdr + "." + claims
	}
	// The signature is not checked by the fake, so a placeholder is enough.
	return hdr + "." + claims + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
}

func do(t *testing.T, method, url, token string, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, b.String()
}

func instToken(t *testing.T, base string) string {
	t.Helper()
	jwt := appJWT(t, "RS256", "12345", time.Now().Add(9*time.Minute), 3)
	resp, body := do(t, "POST", base+"/app/installations/777/access_tokens", jwt, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("access_tokens: %d %s", resp.StatusCode, body)
	}
	var out struct{ Token string }
	_ = json.Unmarshal([]byte(body), &out)
	return out.Token
}

func TestAccessTokens(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	if tok := instToken(t, ts.URL); !strings.HasPrefix(tok, "fake-inst-") {
		t.Fatalf("token %q lacks fake-inst- prefix", tok)
	}
	bad := map[string]string{
		"alg none":  appJWT(t, "none", "1", time.Now().Add(time.Minute), 3),
		"expired":   appJWT(t, "RS256", "1", time.Now().Add(-time.Minute), 3),
		"two parts": appJWT(t, "RS256", "1", time.Now().Add(time.Minute), 2),
		"empty iss": appJWT(t, "RS256", "", time.Now().Add(time.Minute), 3),
		"no bearer": "",
		"not a jwt": "garbage",
	}
	for name, jwt := range bad {
		if resp, _ := do(t, "POST", ts.URL+"/app/installations/777/access_tokens", jwt, ""); resp.StatusCode != 401 {
			t.Errorf("%s: got %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestTeamControl(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	path := "/orgs/ephematool-e2e/teams/creators/memberships/"
	if resp, body := do(t, "GET", ts.URL+path+"octo-creator", inst, ""); resp.StatusCode != 200 || !strings.Contains(body, "active") {
		t.Fatalf("octo-creator: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, "GET", ts.URL+path+"octo-viewer", inst, ""); resp.StatusCode != 404 {
		t.Fatalf("octo-viewer: got %d, want 404", resp.StatusCode)
	}
	resp, _ := do(t, "PUT", ts.URL+"/__fake/teams/ephematool-e2e/creators/memberships/octo-demoted", "", `{"state":"none"}`)
	if resp.StatusCode != 204 {
		t.Fatalf("PUT team: %d", resp.StatusCode)
	}
	if resp, _ := do(t, "GET", ts.URL+path+"octo-demoted", inst, ""); resp.StatusCode != 404 {
		t.Fatalf("octo-demoted after removal: got %d, want 404", resp.StatusCode)
	}
	if resp, _ := do(t, "GET", ts.URL+path+"octo-creator", "", ""); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated: got %d, want 401", resp.StatusCode)
	}
}

func refSHA(t *testing.T, base, tok, repo, branch string) (int, string) {
	t.Helper()
	resp, body := do(t, "GET", base+"/repos/"+repo+"/git/ref/heads/"+branch, tok, "")
	var out struct {
		Object struct{ SHA string }
	}
	_ = json.Unmarshal([]byte(body), &out)
	return resp.StatusCode, out.Object.SHA
}

func TestRefsAndCommits(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	repo := "ephematool-e2e/demo-app"

	st, mainSHA := refSHA(t, ts.URL, inst, repo, "main")
	if st != 200 || len(mainSHA) != 40 {
		t.Fatalf("main: %d %q", st, mainSHA)
	}
	if _, again := refSHA(t, ts.URL, inst, repo, "main"); again != mainSHA {
		t.Fatal("sha not stable")
	}
	if st, sha := refSHA(t, ts.URL, inst, repo, "feature/x"); st != 200 || sha == mainSHA {
		t.Fatalf("feature/x: %d %q", st, sha)
	}
	if st, _ := refSHA(t, ts.URL, inst, repo, "missing-x"); st != 404 {
		t.Fatalf("missing-x: %d", st)
	}
	if st, _ := refSHA(t, ts.URL, "", repo, "main"); st != 401 {
		t.Fatalf("unauthenticated: %d", st)
	}
	if st, _ := refSHA(t, ts.URL, inst, "nope/none", "main"); st != 404 {
		t.Fatalf("unknown repo: %d", st)
	}

	resp, body := do(t, "GET", ts.URL+"/repos/"+repo+"/commits/"+mainSHA[:7], inst, "")
	if resp.StatusCode != 200 || !strings.Contains(body, mainSHA) {
		t.Fatalf("short sha: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, "GET", ts.URL+"/repos/"+repo+"/commits/deadbee", inst, ""); resp.StatusCode != 404 {
		t.Fatalf("unknown sha: %d", resp.StatusCode)
	}
}

func TestPulls(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	_, featureSHA := refSHA(t, ts.URL, inst, "ephematool-e2e/demo-app", "feature/x")
	get := func(n string) (int, map[string]any) {
		resp, body := do(t, "GET", ts.URL+"/repos/ephematool-e2e/demo-app/pulls/"+n, inst, "")
		var m map[string]any
		_ = json.Unmarshal([]byte(body), &m)
		return resp.StatusCode, m
	}
	_, p1 := get("1")
	head := p1["head"].(map[string]any)
	if head["sha"] != featureSHA || head["repo"].(map[string]any)["full_name"] != "ephematool-e2e/demo-app" {
		t.Errorf("pr1 head = %v", head)
	}
	_, p2 := get("2")
	if p2["head"].(map[string]any)["repo"].(map[string]any)["full_name"] != "octo-fork/demo-app" {
		t.Errorf("pr2 must come from the fork: %v", p2["head"])
	}
	_, p3 := get("3")
	if p3["head"].(map[string]any)["repo"] != nil {
		t.Errorf("pr3 head.repo must be null: %v", p3["head"])
	}
	_, p4 := get("4")
	if p4["state"] != "closed" {
		t.Errorf("pr4 state = %v", p4["state"])
	}
	if st, _ := get("99"); st != 404 {
		t.Errorf("pr99: %d", st)
	}
}

func TestRegistry(t *testing.T) {
	ts := httptest.NewServer(New())
	defer ts.Close()
	inst := instToken(t, ts.URL)
	_, mainSHA := refSHA(t, ts.URL, inst, "ephematool-e2e/demo-app", "main")
	_, noimage := refSHA(t, ts.URL, inst, "ephematool-e2e/demo-app", "noimage-x")
	manifest := func(method, repo, tag, tok string) (*http.Response, string) {
		return do(t, method, ts.URL+"/v2/"+repo+"/manifests/"+tag, tok, "")
	}

	resp, _ := manifest("GET", "ephematool-e2e/demo-app", mainSHA, "")
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer realm=") {
		t.Fatalf("no bearer: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	_, tokBody := do(t, "GET", ts.URL+"/token?scope=repository:ephematool-e2e/demo-app:pull", "", "")
	var tok struct{ Token string }
	_ = json.Unmarshal([]byte(tokBody), &tok)

	get, _ := manifest("GET", "ephematool-e2e/demo-app", mainSHA, tok.Token)
	if get.StatusCode != 200 || !strings.HasPrefix(get.Header.Get("Docker-Content-Digest"), "sha256:") {
		t.Fatalf("manifest: %d %v", get.StatusCode, get.Header)
	}
	head, hb := manifest("HEAD", "ephematool-e2e/demo-app", mainSHA, tok.Token)
	if head.StatusCode != 200 || hb != "" || head.Header.Get("Docker-Content-Digest") != get.Header.Get("Docker-Content-Digest") {
		t.Fatalf("HEAD must mirror GET with an empty body: %d %q", head.StatusCode, hb)
	}
	if r, _ := manifest("GET", "ephematool-e2e/demo-app", noimage, tok.Token); r.StatusCode != 404 {
		t.Fatalf("noimage: %d", r.StatusCode)
	}
	if r, _ := manifest("GET", "somebody/else", mainSHA, tok.Token); r.StatusCode != 403 {
		t.Fatalf("unknown repo: %d", r.StatusCode)
	}
	if r, _ := manifest("GET", "ephematool-e2e/demo-app", mainSHA[:7], tok.Token); r.StatusCode != 200 {
		t.Fatalf("sha7 tag: %d", r.StatusCode)
	}
}
