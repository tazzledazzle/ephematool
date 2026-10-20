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
	"time"
)

const (
	// ClientID and ClientSecret are the fake OAuth App credentials.
	ClientID     = "e2e-client"
	ClientSecret = "e2e-secret"
	// Org is the fixture organization; Team is the fixture "org/slug" team.
	Org  = "ephematool-e2e"
	Team = "partner-org/sre"
	// CreatorsTeam is the fixture "org/slug" whose members may create environments.
	CreatorsTeam = "ephematool-e2e/creators"
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
		// Phase 2: members of the creators team. octo-demoted is mutated by tests
		// through SetTeamMembership to prove the role is re-checked per request.
		{Login: "octo-creator", ID: 1007, Orgs: map[string]string{Org: "active"}, Teams: map[string]string{CreatorsTeam: "active"}},
		{Login: "octo-demoted", ID: 1008, Orgs: map[string]string{Org: "active"}, Teams: map[string]string{CreatorsTeam: "active"}},
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

	// Phase 2 state, all guarded by mu. Defined in repos.go and actions.go.
	scenarios  map[string]string // known commit sha -> scenario (success, fail, ...)
	instTokens int               // counter for fake-inst-* tokens
	runs       map[int64]*run
	nextRunID  int64
	dispatches []Dispatch
	now        func() time.Time
	stopStep   chan struct{}
}

// New returns a fake GitHub with the canonical fixtures.
func New() *Server {
	s := &Server{
		mux:      http.NewServeMux(),
		fixtures: map[string]Fixture{},
		codes:    map[string]grant{},

		scenarios: map[string]string{},
		runs:      map[int64]*run{},
		nextRunID: 9000000,
		now:       time.Now,
	}
	for _, f := range Fixtures() {
		s.fixtures[f.Login] = f
	}
	s.mux.HandleFunc("GET /login/oauth/authorize", s.authorize)
	s.mux.HandleFunc("POST /login/oauth/access_token", s.accessToken)
	s.mux.HandleFunc("GET /user", s.user)
	s.mux.HandleFunc("GET /user/memberships/orgs/{org}", s.orgMembership)
	s.mux.HandleFunc("GET /orgs/{org}/teams/{slug}/memberships/{login}", s.teamMembership)
	s.mux.HandleFunc("PUT /__fake/teams/{org}/{slug}/memberships/{login}", s.setTeam)
	s.registerApp()
	s.registerRepos()
	s.registerRegistry()
	s.registerActions()
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
		s.mu.Lock()
		_, known := s.fixtures[login]
		s.mu.Unlock()
		if !known {
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

	s.mu.Lock()
	logins := make([]string, 0, len(s.fixtures))
	for l := range s.fixtures {
		logins = append(logins, l)
	}
	s.mu.Unlock()
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
	s.mu.Lock()
	f, ok := s.fixtures[strings.TrimPrefix(tok, "fake-token-")]
	s.mu.Unlock()
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
	if !s.appOrUser(w, r) { // installation tokens (platform) or user tokens (login flow)
		return
	}
	s.mu.Lock()
	f, ok := s.fixtures[r.PathValue("login")]
	state, member := f.Teams[r.PathValue("org")+"/"+r.PathValue("slug")]
	s.mu.Unlock()
	if !ok || !member || state == "none" {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": state, "role": "member"})
}

// SetTeamMembership changes a fixture's membership in "org/slug". state "none"
// removes it (the next GET answers 404); anything else is stored as given.
func (s *Server) SetTeamMembership(org, slug, login, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixtures[login]
	if !ok {
		return
	}
	teams := map[string]string{}
	for k, v := range f.Teams {
		teams[k] = v
	}
	if state == "none" {
		delete(teams, org+"/"+slug)
	} else {
		teams[org+"/"+slug] = state
	}
	f.Teams = teams
	s.fixtures[login] = f
}

func (s *Server) setTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.State == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "state required"})
		return
	}
	s.SetTeamMembership(r.PathValue("org"), r.PathValue("slug"), r.PathValue("login"), body.State)
	w.WriteHeader(http.StatusNoContent)
}
