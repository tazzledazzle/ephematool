package fakegithub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dispatch is one recorded workflow_dispatch call.
type Dispatch struct {
	File   string
	Ref    string
	Inputs map[string]string
	At     time.Time
}

// run is one fake workflow run: queued -> in_progress -> completed.
type run struct {
	id         int64
	repo       string
	title      string
	inputs     map[string]string
	scenario   string
	status     string // queued | in_progress | completed
	conclusion string // "" until completed
	version    int
	created    time.Time
	started    time.Time
	updated    time.Time
}

const (
	maxInputs     = 25
	maxInputChars = 65535
)

func (s *Server) registerActions() {
	s.mux.HandleFunc("POST /repos/{o}/{r}/actions/workflows/{file}/dispatches", s.dispatch)
	s.mux.HandleFunc("GET /repos/{o}/{r}/actions/runs", s.listRuns)
	s.mux.HandleFunc("GET /repos/{o}/{r}/actions/runs/{id}", s.getRun)
	s.mux.HandleFunc("GET /repos/{o}/{r}/actions/runs/{id}/jobs", s.runJobs)
	s.mux.HandleFunc("POST /__fake/runs/{id}/advance", s.advanceHTTP)
	s.mux.HandleFunc("GET /__fake/dispatches", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Dispatches())
	})
	// Stand-in for the preview gateway that the worker probes in the e2e stack.
	s.mux.HandleFunc("GET /__ephematool/healthz", s.previewHealth)
}

func (s *Server) htmlURL(r *http.Request, repo string, id int64) string {
	return fmt.Sprintf("http://%s/%s/actions/runs/%d", r.Host, repo, id)
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	repo := r.PathValue("o") + "/" + r.PathValue("r")
	if !s.repoKnown(repo) {
		notFound(w)
		return
	}
	var body struct {
		Ref              string         `json:"ref"`
		Inputs           map[string]any `json:"inputs"`
		ReturnRunDetails bool           `json:"return_run_details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "ref required"})
		return
	}
	inputs := map[string]string{}
	total := 0
	for k, v := range body.Inputs {
		sv := fmt.Sprint(v)
		inputs[k] = sv
		total += len(sv)
	}
	if len(inputs) > maxInputs || total > maxInputChars {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"message": "Invalid Request. Too many inputs or inputs too large"})
		return
	}
	scn := s.scenario(inputs["sha"])

	s.mu.Lock()
	s.nextRunID++
	now := s.now()
	ru := &run{
		id: s.nextRunID, repo: repo, inputs: inputs, scenario: scn,
		title:  "create " + inputs["env_id"] + " " + inputs["correlation_id"],
		status: "queued", version: 1, created: now, updated: now,
	}
	s.runs[ru.id] = ru
	s.dispatches = append(s.dispatches, Dispatch{File: r.PathValue("file"), Ref: body.Ref, Inputs: inputs, At: now})
	id := ru.id
	s.mu.Unlock()

	// GitHub API 2022-11-28 answers 200 only when return_run_details is set;
	// 2026-03-10 always answers 200. The no-runid scenario always answers 204
	// so the client's correlation-search fallback can be exercised.
	wantsDetails := body.ReturnRunDetails || r.Header.Get("X-GitHub-Api-Version") == "2026-03-10"
	if scn == "no-runid" || !wantsDetails {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflow_run_id": id,
		"run_url":         fmt.Sprintf("http://%s/repos/%s/actions/runs/%d", r.Host, repo, id),
		"html_url":        s.htmlURL(r, repo, id),
	})
}

// runJSON renders a run. Caller holds s.mu.
func (s *Server) runJSON(r *http.Request, ru *run) map[string]any {
	m := map[string]any{
		"id": ru.id, "name": "create-environment", "display_title": ru.title,
		"status": ru.status, "event": "workflow_dispatch",
		"html_url":   s.htmlURL(r, ru.repo, ru.id),
		"created_at": ru.created.UTC().Format(time.RFC3339),
		"updated_at": ru.updated.UTC().Format(time.RFC3339),
	}
	m["conclusion"] = nil
	if ru.conclusion != "" {
		m["conclusion"] = ru.conclusion
	}
	if !ru.started.IsZero() {
		m["run_started_at"] = ru.started.UTC().Format(time.RFC3339)
	}
	return m
}

func etag(ru *run) string { return fmt.Sprintf(`W/"%d-%d"`, ru.id, ru.version) }

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	q := r.URL.Query()
	var since time.Time
	if c := q.Get("created"); c != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimPrefix(c, ">="))
		if err == nil {
			since = t
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*run
	for _, ru := range s.runs {
		if ru.repo != r.PathValue("o")+"/"+r.PathValue("r") {
			continue
		}
		if ev := q.Get("event"); ev != "" && ev != "workflow_dispatch" {
			continue
		}
		// Compare at second resolution like GitHub does.
		if !since.IsZero() && ru.created.Truncate(time.Second).Before(since) {
			continue
		}
		out = append(out, ru)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id > out[j].id })
	runs := make([]map[string]any, 0, len(out))
	for _, ru := range out {
		runs = append(runs, s.runJSON(r, ru))
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(runs), "workflow_runs": runs})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*run, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	ru := s.runs[id]
	s.mu.Unlock()
	if err != nil || ru == nil || ru.repo != r.PathValue("o")+"/"+r.PathValue("r") {
		notFound(w)
		return nil, false
	}
	return ru, true
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	ru, ok := s.lookup(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tag := etag(ru)
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, s.runJSON(r, ru))
}

func (s *Server) runJobs(w http.ResponseWriter, r *http.Request) {
	if !s.appOrUser(w, r) {
		return
	}
	ru, ok := s.lookup(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339)
	}
	base := ru.started
	steps := make([]map[string]any, 0, 3)
	for i, name := range []string{"Validate inputs", "Provision namespace", "Helm install"} {
		st := map[string]any{"name": name, "number": i + 1, "status": ru.status, "conclusion": nil,
			"started_at": nil, "completed_at": nil}
		if !base.IsZero() {
			st["started_at"] = ts(base.Add(time.Duration(i) * time.Second))
		}
		if ru.status == "completed" {
			st["conclusion"] = ru.conclusion
			st["completed_at"] = ts(base.Add(time.Duration(i+1) * time.Second))
		}
		steps = append(steps, st)
	}
	job := map[string]any{"name": "provision", "status": ru.status, "conclusion": nil,
		"started_at": ts(base), "completed_at": nil, "steps": steps}
	if ru.status == "completed" {
		job["conclusion"] = ru.conclusion
		job["completed_at"] = ts(base.Add(3 * time.Second))
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": 1, "jobs": []any{job}})
}

// advance performs one lifecycle transition. Caller holds s.mu.
func (s *Server) advance(ru *run) {
	switch ru.status {
	case "queued":
		ru.status = "in_progress"
		ru.started = s.now()
	case "in_progress":
		if ru.scenario == "stall" {
			return // stays in progress forever
		}
		ru.status = "completed"
		ru.conclusion = map[string]string{"fail": "failure", "cancel": "cancelled"}[ru.scenario]
		if ru.conclusion == "" {
			ru.conclusion = "success"
		}
	default:
		return
	}
	ru.version++
	ru.updated = s.now()
}

// AdvanceRun performs one transition on a run and reports whether it exists.
func (s *Server) AdvanceRun(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ru, ok := s.runs[id]
	if ok {
		s.advance(ru)
	}
	return ok
}

func (s *Server) advanceHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || !s.AdvanceRun(id) {
		notFound(w)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.runJSON(r, s.runs[id]))
}

// SetRunStep auto-advances every non-terminal run each d. d == 0 stops it
// (manual mode). Safe to call repeatedly.
func (s *Server) SetRunStep(d time.Duration) {
	s.mu.Lock()
	if s.stopStep != nil {
		close(s.stopStep)
		s.stopStep = nil
	}
	if d <= 0 {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.stopStep = stop
	s.mu.Unlock()
	go func() {
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.mu.Lock()
				for _, ru := range s.runs {
					s.advance(ru)
				}
				s.mu.Unlock()
			}
		}
	}()
}

// Dispatches returns a copy of every recorded workflow_dispatch call.
func (s *Server) Dispatches() []Dispatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Dispatch(nil), s.dispatches...)
}

// previewHealth stands in for the preview gateway: 200 only after the run for
// the env named by the Host's first label completed successfully.
func (s *Server) previewHealth(w http.ResponseWriter, r *http.Request) {
	envID, _, _ := strings.Cut(r.Host, ".")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ru := range s.runs {
		if ru.inputs["env_id"] == envID && ru.conclusion == "success" {
			_, _ = w.Write([]byte("ok"))
			return
		}
	}
	http.Error(w, "not ready", http.StatusServiceUnavailable)
}
