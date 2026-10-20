package fakegithub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// registerApp wires the GitHub App installation-token endpoint.
func (s *Server) registerApp() {
	s.mux.HandleFunc("POST /app/installations/{id}/access_tokens", s.accessTokens)
}

// bearer returns the token from an Authorization header ("Bearer x" or "token x").
func bearer(r *http.Request) string {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.TrimPrefix(tok, "token ")
}

// checkAppJWT validates an App JWT *structurally*: three dot-separated parts,
// header alg RS256, payload with a non-empty iss, an exp in the future and an
// iat not more than 60s ahead. The signature is deliberately not checked, so no
// private key ever needs to be committed (T-01-26: test-only fake).
func checkAppJWT(tok string, now time.Time) error {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return fmt.Errorf("want 3 parts, got %d", len(parts))
	}
	dec := func(p string, v any) error {
		raw, err := base64.RawURLEncoding.DecodeString(p)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, v)
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if err := dec(parts[0], &hdr); err != nil || hdr.Alg != "RS256" {
		return fmt.Errorf("alg must be RS256")
	}
	var claims struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
	}
	if err := dec(parts[1], &claims); err != nil || claims.Iss == "" {
		return fmt.Errorf("iss required")
	}
	if claims.Exp <= now.Unix() {
		return fmt.Errorf("expired")
	}
	if claims.Iat > now.Add(60*time.Second).Unix() {
		return fmt.Errorf("iat in the future")
	}
	return nil
}

func (s *Server) accessTokens(w http.ResponseWriter, r *http.Request) {
	if err := checkAppJWT(bearer(r), s.now()); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	s.mu.Lock()
	s.instTokens++
	n := s.instTokens
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{
		"token":      fmt.Sprintf("fake-inst-%s-%d", r.PathValue("id"), n),
		"expires_at": s.now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
}

// InstallationTokens reports how many installation tokens were minted. Tests
// use it to prove the client caches its token.
func (s *Server) InstallationTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instTokens
}

// appOrUser accepts a fake installation token or a fake user token. It writes
// the 401 itself and reports whether the caller may continue.
func (s *Server) appOrUser(w http.ResponseWriter, r *http.Request) bool {
	tok := bearer(r)
	if strings.HasPrefix(tok, "fake-inst-") {
		return true
	}
	_, ok := s.caller(w, r)
	return ok
}
