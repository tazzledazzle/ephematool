package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/oauth2"

	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/store"
)

const oauthCookie = "ephematool_oauth"

// RouteWrapper tags a plain handler with its route template. httpapi supplies
// httpapi.RouteHandler; passing it in avoids an httpapi <-> auth import cycle.
type RouteWrapper func(method, pattern string, h http.Handler) http.Handler

// Handlers serves the login flow and the session-backed API operations.
type Handlers struct {
	cfg    config.Config
	sm     *scs.SessionManager
	q      *store.Queries
	oauth  *oauth2.Config
	hc     *http.Client
	allow  Allowlist
	logins metric.Int64Counter
}

// NewHandlers builds the handlers. sm and q may be nil when only the OpenAPI
// spec is generated.
func NewHandlers(cfg config.Config, sm *scs.SessionManager, q *store.Queries) *Handlers {
	logins, _ := otel.Meter("ephematool/auth").Int64Counter("ephematool.auth.logins",
		metric.WithDescription("GitHub login attempts by outcome"))
	return &Handlers{
		cfg: cfg, sm: sm, q: q,
		oauth:  newOAuthConfig(cfg),
		hc:     newHTTPClient(),
		allow:  Allowlist{Orgs: cfg.AllowedOrgs, Teams: cfg.AllowedTeams},
		logins: logins,
	}
}

// Sessions exposes the session manager (used by httpapi for LoadAndSave).
func (h *Handlers) Sessions() *scs.SessionManager { return h.sm }

func (h *Handlers) count(ctx context.Context, outcome string) {
	if h.logins != nil {
		h.logins.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}

func (h *Handlers) fail(w http.ResponseWriter, r *http.Request, reason string) {
	slog.WarnContext(r.Context(), "auth.failed", "reason", reason)
	h.count(r.Context(), "failed")
	http.Redirect(w, r, "/login?reason=failed", http.StatusFound)
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (h *Handlers) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: oauthCookie, Value: value, Path: "/api/auth/github", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.cfg.CookieSecure,
	}
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.GitHubConfigured() {
		var missing []string
		if h.cfg.GitHubClientID == "" {
			missing = append(missing, "GITHUB_CLIENT_ID")
		}
		if h.cfg.GitHubClientSecret == "" {
			missing = append(missing, "GITHUB_CLIENT_SECRET")
		}
		slog.WarnContext(r.Context(), "auth.github_not_configured", "missing", strings.Join(missing, ","))
		h.count(r.Context(), "failed")
		http.Redirect(w, r, "/login?reason=failed", http.StatusFound)
		return
	}
	state := randomString(32)
	verifier := oauth2.GenerateVerifier()
	next := base64.RawURLEncoding.EncodeToString([]byte(SafeNext(r.URL.Query().Get("next"))))
	http.SetCookie(w, h.cookie(state+"|"+verifier+"|"+next, 600))
	http.Redirect(w, r, h.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (h *Handlers) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, cerr := r.Cookie(oauthCookie)
	http.SetCookie(w, h.cookie("", -1))
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		h.fail(w, r, "provider_"+e)
		return
	}
	if cerr != nil {
		h.fail(w, r, "missing_oauth_cookie")
		return
	}
	parts := strings.SplitN(c.Value, "|", 3)
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 || parts[0] == "" {
		h.fail(w, r, "state_mismatch")
		return
	}
	verifier := parts[1]
	nb, _ := base64.RawURLEncoding.DecodeString(parts[2])
	next := SafeNext(string(nb))
	code := q.Get("code")
	if code == "" {
		h.fail(w, r, "missing_code")
		return
	}

	// The access token is a local variable only: never stored, logged or traced.
	xctx := context.WithValue(ctx, oauth2.HTTPClient, h.hc)
	tok, err := h.oauth.Exchange(xctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		h.fail(w, r, "token_exchange")
		return
	}
	gh, err := newGitHubClient(h.hc, h.cfg.GitHubAPIURL, tok.AccessToken)
	if err != nil {
		h.fail(w, r, "github_client")
		return
	}
	gu, _, err := gh.Users.Get(ctx, "")
	if err != nil || gu.GetLogin() == "" {
		h.fail(w, r, "github_user")
		return
	}
	login, id := gu.GetLogin(), gu.GetID()

	dec := h.allow.Evaluate(ctx, GitHubMembershipClient{Client: gh}, login)
	if !dec.Allowed {
		slog.WarnContext(ctx, "auth.denied", "login", login, "github_id", id, "reason", dec.Reason,
			"required_orgs", strings.Join(h.cfg.AllowedOrgs, ","), "required_teams", strings.Join(h.cfg.AllowedTeams, ","))
		h.count(ctx, "denied")
		http.Redirect(w, r, "/denied?reason=org_membership&login="+url.QueryEscape(login), http.StatusFound)
		return
	}

	if _, err := h.q.UpsertUser(ctx, store.UpsertUserParams{GithubID: id, Login: login, AvatarUrl: gu.GetAvatarURL()}); err != nil {
		h.fail(w, r, "user_upsert")
		return
	}
	role := RoleFor(login, h.cfg.AdminLogins)
	if err := h.sm.RenewToken(ctx); err != nil { // session fixation defense
		h.fail(w, r, "session_renew")
		return
	}
	h.sm.Put(ctx, KeyUserID, id)
	h.sm.Put(ctx, KeyLogin, login)
	h.sm.Put(ctx, KeyAvatarURL, gu.GetAvatarURL())
	h.sm.Put(ctx, KeyRole, role)
	h.sm.Put(ctx, KeyOrgs, dec.GrantedBy)
	slog.InfoContext(ctx, "auth.login", "login", login, "github_id", id, "role", role)
	h.count(ctx, "success")
	http.Redirect(w, r, next, http.StatusFound)
}

// MeBody is the GET /api/me payload.
type MeBody struct {
	Login     string   `json:"login"`
	AvatarURL string   `json:"avatarUrl"`
	Role      string   `json:"role" enum:"admin,viewer"`
	Orgs      []string `json:"orgs"`
}

// MeResponse wraps MeBody for Huma.
type MeResponse struct{ Body MeBody }

// ConfigBody is the GET /api/auth/config payload.
type ConfigBody struct {
	GitHubConfigured bool     `json:"githubConfigured"`
	AllowedOrgs      []string `json:"allowedOrgs"`
	AllowedTeams     []string `json:"allowedTeams"`
}

// ConfigResponse wraps ConfigBody for Huma.
type ConfigResponse struct{ Body ConfigBody }

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Register wires the plain OAuth routes (via wrap) and the Huma operations.
func Register(api huma.API, mux *http.ServeMux, h *Handlers, wrap RouteWrapper) {
	oa := api.OpenAPI()
	if oa.Components == nil {
		oa.Components = &huma.Components{}
	}
	if oa.Components.SecuritySchemes == nil {
		oa.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	oa.Components.SecuritySchemes["session"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: CookieName}
	api.UseMiddleware(RequireSession(api, h.sm))

	mux.Handle("GET /api/auth/github/login", wrap("GET", "/api/auth/github/login", http.HandlerFunc(h.login)))
	mux.Handle("GET /api/auth/github/callback", wrap("GET", "/api/auth/github/callback", http.HandlerFunc(h.callback)))

	huma.Register(api, huma.Operation{
		OperationID: "get-me", Method: http.MethodGet, Path: "/api/me",
		Summary: "Current signed-in user", Security: []map[string][]string{{"session": {}}},
	}, func(ctx context.Context, _ *struct{}) (*MeResponse, error) {
		orgs, _ := h.sm.Get(ctx, KeyOrgs).([]string)
		id := h.sm.GetInt64(ctx, KeyUserID)
		if h.q != nil { // read the persisted user (first real DB read)
			if u, err := h.q.GetUserByGithubID(ctx, id); err == nil {
				return &MeResponse{Body: MeBody{
					Login: u.Login, AvatarURL: u.AvatarUrl, Role: h.sm.GetString(ctx, KeyRole), Orgs: nonNil(orgs),
				}}, nil
			}
		}
		return &MeResponse{Body: MeBody{
			Login: h.sm.GetString(ctx, KeyLogin), AvatarURL: h.sm.GetString(ctx, KeyAvatarURL),
			Role: h.sm.GetString(ctx, KeyRole), Orgs: nonNil(orgs),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-config", Method: http.MethodGet, Path: "/api/auth/config",
		Summary: "Public sign-in configuration",
	}, func(context.Context, *struct{}) (*ConfigResponse, error) {
		return &ConfigResponse{Body: ConfigBody{
			GitHubConfigured: h.cfg.GitHubConfigured(),
			AllowedOrgs:      nonNil(h.cfg.AllowedOrgs),
			AllowedTeams:     nonNil(h.cfg.AllowedTeams),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-logout", Method: http.MethodPost, Path: "/api/auth/logout",
		Summary: "Sign out", DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if h.sm != nil {
			if err := h.sm.Destroy(ctx); err != nil {
				return nil, huma.Error500InternalServerError("could not end session")
			}
		}
		return nil, nil
	})
}
