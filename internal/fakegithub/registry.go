package fakegithub

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

const registryToken = "fake-registry"

// registerRegistry wires a minimal GHCR: token endpoint plus manifest GET/HEAD.
func (s *Server) registerRegistry() {
	s.mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"token": registryToken})
	})
	// A "GET" pattern also serves HEAD; net/http drops the body for HEAD.
	s.mux.HandleFunc("GET /v2/{org}/{repo}/manifests/{tag}", s.manifest)
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	if bearer(r) != registryToken {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="ghcr.io"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []map[string]string{{"code": "UNAUTHORIZED"}}})
		return
	}
	if !s.repoKnown(r.PathValue("org") + "/" + r.PathValue("repo")) {
		writeJSON(w, http.StatusForbidden, map[string]any{"errors": []map[string]string{{"code": "DENIED"}}})
		return
	}
	tag := strings.ToLower(r.PathValue("tag"))
	if !hexRe.MatchString(tag) || !s.tagHasImage(tag) {
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []map[string]string{{"code": "MANIFEST_UNKNOWN"}}})
		return
	}
	sum := sha256.Sum256([]byte(tag))
	w.Header().Set("Docker-Content-Digest", "sha256:"+hex.EncodeToString(sum[:]))
	w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"schemaVersion":2}`))
}

// tagHasImage reports whether tag is a known sha (or a prefix of exactly one)
// whose scenario is not "noimage".
func (s *Server) tagHasImage(tag string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var match string
	n := 0
	for sha := range s.scenarios {
		if strings.HasPrefix(sha, tag) {
			match, n = sha, n+1
		}
	}
	return n == 1 && s.scenarios[match] != "noimage"
}
