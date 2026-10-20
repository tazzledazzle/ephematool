package fakegithub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const envsRepo = "ephematool-e2e/ephematool-environments"

type rig struct {
	t    *testing.T
	srv  *Server
	ts   *httptest.Server
	inst string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	srv := New()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { srv.SetRunStep(0) })
	return &rig{t: t, srv: srv, ts: ts, inst: instToken(t, ts.URL)}
}

func (g *rig) sha(branch string) string {
	_, sha := refSHA(g.t, g.ts.URL, g.inst, "ephematool-e2e/demo-app", branch)
	return sha
}

// dispatch posts a workflow dispatch for envID built from branch.
func (g *rig) dispatch(envID, branch string, extra map[string]any, headers map[string]string) (*http.Response, string) {
	g.t.Helper()
	inputs := map[string]any{"env_id": envID, "correlation_id": "corr-" + envID, "traceparent": "00-aa-bb-01", "sha": g.sha(branch)}
	for k, v := range extra {
		inputs[k] = v
	}
	body, _ := json.Marshal(map[string]any{"ref": "main", "inputs": inputs, "return_run_details": extra["_details"] != false})
	req, _ := http.NewRequest("POST", g.ts.URL+"/repos/"+envsRepo+"/actions/workflows/create-environment.yml/dispatches", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+g.inst)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer resp.Body.Close()
	b := new(strings.Builder)
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	b.Write(buf[:n])
	return resp, b.String()
}

func runID(t *testing.T, body string) int64 {
	t.Helper()
	var out struct {
		ID int64 `json:"workflow_run_id"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.ID == 0 {
		t.Fatalf("no workflow_run_id in %q", body)
	}
	return out.ID
}

func (g *rig) getRun(id int64, ifNoneMatch string) (*http.Response, map[string]any) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/actions/runs/%d", g.ts.URL, envsRepo, id), nil)
	req.Header.Set("Authorization", "Bearer "+g.inst)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func (g *rig) advance(id int64) {
	g.t.Helper()
	resp, _ := do(g.t, "POST", fmt.Sprintf("%s/__fake/runs/%d/advance", g.ts.URL, id), "", "")
	if resp.StatusCode != 200 {
		g.t.Fatalf("advance: %d", resp.StatusCode)
	}
}

func TestDispatchStatusCodes(t *testing.T) {
	g := newRig(t)
	if resp, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil); resp.StatusCode != 200 || !strings.Contains(body, "html_url") {
		t.Fatalf("with return_run_details: %d %s", resp.StatusCode, body)
	}
	if resp, _ := g.dispatch("pv-bbbbbbbbbb", "main", map[string]any{"_details": false}, nil); resp.StatusCode != 204 {
		t.Fatalf("without flag: got %d, want 204", resp.StatusCode)
	}
	if resp, _ := g.dispatch("pv-cccccccccc", "main", map[string]any{"_details": false}, map[string]string{"X-GitHub-Api-Version": "2026-03-10"}); resp.StatusCode != 200 {
		t.Fatalf("api 2026-03-10: got %d, want 200", resp.StatusCode)
	}
	if resp, _ := g.dispatch("pv-dddddddddd", "no-runid-x", nil, nil); resp.StatusCode != 204 {
		t.Fatalf("no-runid: got %d, want 204", resp.StatusCode)
	}
	many := map[string]any{}
	for i := 0; i < 22; i++ { // 4 base inputs + 22 = 26 > 25
		many[fmt.Sprintf("k%d", i)] = "v"
	}
	if resp, _ := g.dispatch("pv-eeeeeeeeee", "main", many, nil); resp.StatusCode != 422 {
		t.Fatalf("26 inputs: got %d, want 422", resp.StatusCode)
	}
	if got := len(g.srv.Dispatches()); got != 4 {
		t.Fatalf("recorded %d dispatches, want 4 (the 422 is not recorded)", got)
	}
}

func TestDispatchRecordsInputsAndListsRun(t *testing.T) {
	g := newRig(t)
	before := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	d := g.srv.Dispatches()
	if len(d) != 1 || d[0].Inputs["env_id"] != "pv-aaaaaaaaaa" || d[0].Inputs["traceparent"] != "00-aa-bb-01" {
		t.Fatalf("dispatch not recorded verbatim: %+v", d)
	}
	resp, body := do(t, "GET", g.ts.URL+"/repos/"+envsRepo+"/actions/runs?event=workflow_dispatch&created=>="+before, g.inst, "")
	if resp.StatusCode != 200 || !strings.Contains(body, "create pv-aaaaaaaaaa corr-pv-aaaaaaaaaa") {
		t.Fatalf("run list: %d %s", resp.StatusCode, body)
	}
}

func TestRunLifecycleAndETag(t *testing.T) {
	g := newRig(t)
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)

	resp, run := g.getRun(id, "")
	tag := resp.Header.Get("ETag")
	if tag == "" || run["status"] != "queued" {
		t.Fatalf("queued run: %q %v", tag, run["status"])
	}
	if resp, _ := g.getRun(id, tag); resp.StatusCode != 304 {
		t.Fatalf("If-None-Match: got %d, want 304", resp.StatusCode)
	}
	g.advance(id)
	resp, run = g.getRun(id, tag)
	if resp.StatusCode != 200 || run["status"] != "in_progress" || resp.Header.Get("ETag") == tag {
		t.Fatalf("after advance: %d %v etag %q", resp.StatusCode, run["status"], resp.Header.Get("ETag"))
	}
	g.advance(id)
	if _, run = g.getRun(id, ""); run["status"] != "completed" || run["conclusion"] != "success" {
		t.Fatalf("completed: %v %v", run["status"], run["conclusion"])
	}
}

func TestScenarios(t *testing.T) {
	g := newRig(t)
	cases := []struct{ branch, status, conclusion string }{
		{"fail-x", "completed", "failure"},
		{"cancel-x", "completed", "cancelled"},
		{"stall-x", "in_progress", ""},
	}
	for i, c := range cases {
		_, body := g.dispatch(fmt.Sprintf("pv-scenario%02d", i), c.branch, nil, nil)
		id := runID(t, body)
		for j := 0; j < 5; j++ {
			g.advance(id)
		}
		_, run := g.getRun(id, "")
		got, _ := run["conclusion"].(string)
		if run["status"] != c.status || got != c.conclusion {
			t.Errorf("%s: status %v conclusion %q, want %s %q", c.branch, run["status"], got, c.status, c.conclusion)
		}
	}
}

func TestJobs(t *testing.T) {
	g := newRig(t)
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)
	g.advance(id)
	g.advance(id)
	_, jb := do(t, "GET", fmt.Sprintf("%s/repos/%s/actions/runs/%d/jobs", g.ts.URL, envsRepo, id), g.inst, "")
	var out struct {
		Jobs []struct {
			Name        string
			Conclusion  string
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
			Steps       []struct {
				Name        string
				Number      int
				StartedAt   time.Time `json:"started_at"`
				CompletedAt time.Time `json:"completed_at"`
			}
		}
	}
	if err := json.Unmarshal([]byte(jb), &out); err != nil || len(out.Jobs) != 1 {
		t.Fatalf("jobs: %v %s", err, jb)
	}
	j := out.Jobs[0]
	if j.Name != "provision" || j.Conclusion != "success" || len(j.Steps) != 3 {
		t.Fatalf("job = %+v", j)
	}
	for i, st := range j.Steps {
		if st.Number != i+1 || !st.CompletedAt.After(st.StartedAt) {
			t.Errorf("step %d timestamps out of order: %+v", i, st)
		}
		if i > 0 && st.StartedAt.Before(j.Steps[i-1].StartedAt) {
			t.Errorf("step %d starts before step %d", i, i-1)
		}
	}
}

func TestPreviewHealth(t *testing.T) {
	g := newRig(t)
	probe := func() int {
		req, _ := http.NewRequest("GET", g.ts.URL+"/__ephematool/healthz", nil)
		req.Host = "pv-aaaaaaaaaa.preview.localhost:8088"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if probe() != 503 {
		t.Fatal("before any run the probe must be 503")
	}
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)
	g.advance(id)
	if probe() != 503 {
		t.Fatal("while in progress the probe must be 503")
	}
	g.advance(id)
	if probe() != 200 {
		t.Fatal("after success the probe must be 200")
	}
}

func TestAutoAdvance(t *testing.T) {
	g := newRig(t)
	g.srv.SetRunStep(20 * time.Millisecond)
	_, body := g.dispatch("pv-aaaaaaaaaa", "main", nil, nil)
	id := runID(t, body)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, run := g.getRun(id, ""); run["conclusion"] == "success" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not complete within 1s of auto-advance")
}
