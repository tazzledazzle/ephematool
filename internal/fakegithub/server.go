// Package fakegithub is a stdlib-only fake of the GitHub OAuth and REST
// endpoints the login flow uses. It is test-only: it ships in tests and in
// deploy/compose.e2e.yaml, never in the base stack (T-01-26).
package fakegithub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// ClientID and ClientSecret are the fake OAuth App credentials.
	ClientID     = "e2e-client"
	ClientSecret = "e2e-secret"
	// Org is the fixture organization; Team is the fixture "org/slug" team.
	Org  = "ephematool-e2e"
	Team = "partner-org/sre"
)

// Fixture describes one fake GitHub user.
type Fixture struct {
	Login string
	ID    int64
	// Orgs maps org -> membership state ("active" | "pending").
	Orgs map[string]string
	// Teams maps "org/slug" -> membership state.
	Teams map[string]string
	// OrgForbidden makes the org membership endpoint return 403.
	OrgForbidden bool
}

// Fixtures returns the canonical fixture users.
func Fixtures() []Fixture {
	return []Fixture{
		{Login: "octo-admin", ID: 1001, Orgs: map[string]string{Org: "active"}},
		{Login: "octo-viewer", ID: 1002, Orgs: map[string]string{Org: "active"}},
		{Login: "octo-teamonly", ID: 1003, Teams: map[string]string{Team: "active"}},
		{Login: "octo-pending", ID: 1004, Orgs: map[string]string{Org: "pending"}},
		{Login: "octo-outsider", ID: 1005},
		{Login: "octo-forbidden", ID: 1006, OrgForbidden: true},
	}
}

type grant struct {
	challenge string
	login     string
}

// Server implements http.Handler.
type Server struct {
	mux      *http.ServeMux
	fixtures map[string]Fixture

	mu            sync.Mutex
	codes         map[string]grant
	lastAuthorize url.Values
}

// New returns a fake GitHub with the canonical fixtures.
func New() *Server {
	s := &Server{
		mux:      http.NewServeMux(),
		fixtures: map[string]Fixture{},
		codes:    map[string]grant{},
	}
	for _, f := range Fixtures() {
		s.fixtures[f.Login] = f
	}
	s.mux.HandleFunc("GET /login/oauth/authorize", s.authorize)
	s.mux.HandleFunc("POST /login/oauth/access_token", s.accessToken)
	s.mux.HandleFunc("GET /user", s.user)
	s.mux.HandleFunc("GET /user/memberships/orgs/{org}", s.orgMembership)
	s.mux.HandleFunc("GET /orgs/{org}/teams/{slug}/memberships/{login}", s.teamMembership)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// LastAuthorize returns the query of the most recent authorize request.
func (s *Server) LastAuthorize() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := url.Values{}
	for k, v := range s.lastAuthorize {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.lastAuthorize = q
	s.mu.Unlock()

	redirect := q.Get("redirect_uri")
	ru, err := url.Parse(redirect)
	if redirect == "" || err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	back := func(extra url.Values) {
		rq := ru.Query()
		rq.Set("state", q.Get("state"))
		for k, v := range extra {
			rq.Set(k, v[0])
		}
		ru.RawQuery = rq.Encode()
		http.Redirect(w, r, ru.String(), http.StatusFound)
	}
	if q.Get("cancel") != "" {
		back(url.Values{"error": {"access_denied"}})
		return
	}
	if login := q.Get("login"); login != "" {
		if _, ok := s.fixtures[login]; !ok {
			http.Error(w, "unknown fixture", http.StatusBadRequest)
			return
		}
		code := randToken()
		s.mu.Lock()
		s.codes[code] = grant{challenge: q.Get("code_challenge"), login: login}
		s.mu.Unlock()
		back(url.Values{"code": {code}})
		return
	}

	logins := make([]string, 0, len(s.fixtures))
	for l := range s.fixtures {
		logins = append(logins, l)
	}
	sort.Strings(logins)
	var b strings.Builder
	b.WriteString("<!doctype html><title>Fake GitHub</title><h1>Authorize (fake GitHub)</h1>")
	b.WriteString(`<form method="get" action="/login/oauth/authorize">`)
	for k, vs := range q {
		if k == "login" || k == "cancel" {
			continue
		}
		b.WriteString(`<input type="hidden" name="` + html.EscapeString(k) + `" value="` + html.EscapeString(vs[0]) + `">`)
	}
	for _, l := range logins {
		b.WriteString(`<p><button type="submit" name="login" value="` + html.EscapeString(l) + `">` + html.EscapeString(l) + `</button></p>`)
	}
	b.WriteString(`<p><button type="submit" name="cancel" value="1">Cancel</button></p></form>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) accessToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	id, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	if u, p, ok := r.BasicAuth(); ok {
		id, secret = u, p
	}
	if id != ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(ClientSecret)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "incorrect_client_credentials"})
		return
	}
	code := r.PostForm.Get("code")
	s.mu.Lock()
	g, ok := s.codes[code]
	delete(s.codes, code) // single use
	s.mu.Unlock()
	verifier := r.PostForm.Get("code_verifier")
	if !ok || g.challenge == "" || verifier == "" ||
		subtle.ConstantTimeCompare([]byte(s256(verifier)), []byte(g.challenge)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_verification_code"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": "fake-token-" + g.login,
		"token_type":   "bearer",
		"scope":        "read:org",
	})
}

// caller resolves the bearer token to a fixture.
func (s *Server) caller(w http.ResponseWriter, r *http.Request) (Fixture, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	tok = strings.TrimPrefix(tok, "token ")
	f, ok := s.fixtures[strings.TrimPrefix(tok, "fake-token-")]
	if !ok || !strings.HasPrefix(tok, "fake-token-") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return Fixture{}, false
	}
	return f, true
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	f, ok := s.caller(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"login":      f.Login,
		"id":         f.ID,
		"avatar_url": "https://avatars.example.invalid/u/" + strconv.FormatInt(f.ID, 10),
	})
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func (s *Server) orgMembership(w http.ResponseWriter, r *http.Request) {
	f, ok := s.caller(w, r)
	if !ok {
		return
	}
	if f.OrgForbidden {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible"})
		return
	}
	org := r.PathValue("org")
	state, ok := f.Orgs[org]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":        state,
		"role":         "member",
		"organization": map[string]string{"login": org},
		"user":         map[string]string{"login": f.Login},
	})
}

func (s *Server) teamMembership(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.caller(w, r); !ok {
		return
	}
	f, ok := s.fixtures[r.PathValue("login")]
	if !ok {
		notFound(w)
		return
	}
	state, ok := f.Teams[r.PathValue("org")+"/"+r.PathValue("slug")]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": state, "role": "member"})
}
