package fakegithub

import (
	"crypto/sha1" //nolint:gosec // not security: deterministic fake commit ids
	"encoding/hex"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Repos is the fixed set of repositories the fake knows (all lowercase).
var Repos = []string{
	"ephematool-e2e/demo-app",
	"ephematool-e2e/other-app",
	"ephematool-e2e/ephematool-environments",
}

var (
	branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	hexRe    = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// scenarioPrefixes are branch-name prefixes that select how a build behaves.
// The scenario is remembered per resolved commit sha.
var scenarioPrefixes = map[string]string{
	"fail-": "fail", "stall-": "stall", "cancel-": "cancel",
	"no-runid-": "no-runid", "noimage-": "noimage",
}

func (s *Server) repoKnown(repo string) bool {
	for _, r := range Repos {
		if strings.EqualFold(r, repo) {
			return true
		}
	}
	return false
}

// shaFor derives the stable fake commit id for a branch in a repo.
func shaFor(repo, branch string) string {
	sum := sha1.Sum([]byte(strings.ToLower(repo) + ":" + branch)) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

// remember records the scenario for a sha (default "success") and returns sha.
func (s *Server) remember(sha, branch string) string {
	scn := "success"
	for prefix, name := range scenarioPrefixes {
		if strings.HasPrefix(branch, prefix) {
			scn = name
		}
	}
	s.mu.Lock()
	s.scenarios[sha] = scn
	s.mu.Unlock()
	return sha
}

func (s *Server) scenario(sha string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scn, ok := s.scenarios[sha]; ok {
		return scn
	}
	return "success"
}

func (s *Server) registerRepos() {
	// The three fixed branches exist for every known repo from startup.
	for _, repo := range Repos {
		for _, b := range []string{"main", "feature/x", "release/1.2"} {
			s.remember(shaFor(repo, b), b)
		}
	}
	s.mux.HandleFunc("GET /repos/{o}/{r}/git/ref/heads/{b...}", s.getRef)
	s.mux.HandleFunc("GET /repos/{o}/{r}/commits/{ref}", s.getCommit)
	s.mux.HandleFunc("GET /repos/{o}/{r}/pulls/{n}", s.getPull)
}

func (s *Server) getRef(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	repo := r.PathValue("o") + "/" + r.PathValue("r")
	branch := r.PathValue("b")
	if !s.repoKnown(repo) || !branchRe.MatchString(branch) || strings.HasPrefix(branch, "missing-") {
		notFound(w)
		return
	}
	sha := s.remember(shaFor(repo, branch), branch)
	writeJSON(w, http.StatusOK, map[string]any{
		"ref":    "refs/heads/" + branch,
		"object": map[string]string{"type": "commit", "sha": sha},
	})
}

func (s *Server) getCommit(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	ref := strings.ToLower(r.PathValue("ref"))
	if !hexRe.MatchString(ref) {
		notFound(w)
		return
	}
	s.mu.Lock()
	var found []string
	for sha := range s.scenarios {
		if strings.HasPrefix(sha, ref) {
			found = append(found, sha)
		}
	}
	s.mu.Unlock()
	if len(found) != 1 { // unknown, or an ambiguous prefix
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sha": found[0]})
}

// getPull serves four fixture pull requests: 1 same-repo open, 2 from a fork,
// 3 from a deleted fork (head.repo null), 4 closed. Anything else is 404.
func (s *Server) getPull(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	repo := r.PathValue("o") + "/" + r.PathValue("r")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || !s.repoKnown(repo) || n < 1 || n > 4 {
		notFound(w)
		return
	}
	headRepo := any(map[string]string{"full_name": repo})
	headSHA := s.remember(shaFor(repo, "feature/x"), "feature/x")
	state := "open"
	switch n {
	case 2:
		fork := "octo-fork/demo-app"
		headRepo = map[string]string{"full_name": fork}
		headSHA = s.remember(shaFor(fork, "feature/x"), "feature/x")
	case 3:
		headRepo = nil
	case 4:
		state = "closed"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"number": n,
		"state":  state,
		"head":   map[string]any{"ref": "feature/x", "sha": headSHA, "repo": headRepo},
		"base":   map[string]any{"ref": "main", "repo": map[string]string{"full_name": repo}},
	})
}
