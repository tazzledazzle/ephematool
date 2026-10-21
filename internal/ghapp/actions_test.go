package ghapp

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func dispatchInput(t *testing.T, f *fixture, envID, branch string) DispatchInput {
	t.Helper()
	sha, err := f.c.ResolveRef(t.Context(), demoRepo, RefBranch, branch)
	if err != nil {
		t.Fatal(err)
	}
	return DispatchInput{
		EnvID: envID, CorrelationID: "corr-" + envID, Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		Repo: demoRepo, SHA: sha, Image: "ghcr.io/ephematool-e2e/demo-app@sha256:" + fmt.Sprintf("%064d", 1),
		Template: "web-app", ChartVersion: "0.1.0", Size: "S", ValuesB64: "e30=", Owner: "octo-admin",
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}
}

func TestDispatchDefaultScenario(t *testing.T) {
	f := newFixture(t)
	res, err := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-aaaaaaaaaa", "main"))
	if err != nil || res.RunID == 0 || res.HTMLURL == "" || res.Ambiguous {
		t.Fatalf("Dispatch = %+v %v, want a run id and no ambiguity", res, err)
	}
	d := f.fake.Dispatches()
	if len(d) != 1 || len(d[0].Inputs) != 12 || d[0].Inputs["env_id"] != "pv-aaaaaaaaaa" || d[0].Inputs["size"] != "S" {
		t.Fatalf("fake recorded %+v, want exactly 12 inputs", d)
	}
}

func TestDispatchAmbiguousThenCorrelate(t *testing.T) {
	f := newFixture(t)
	start := time.Now()
	res, err := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-bbbbbbbbbb", "no-runid-x"))
	if err != nil || !res.Ambiguous || res.RunID != 0 {
		t.Fatalf("Dispatch = %+v %v, want Ambiguous", res, err)
	}
	run, found, err := f.c.FindRunByCorrelation(t.Context(), "corr-pv-bbbbbbbbbb", start)
	if err != nil || !found || run.ID == 0 {
		t.Fatalf("FindRunByCorrelation = %+v %v %v", run, found, err)
	}
	if _, found, _ := f.c.FindRunByCorrelation(t.Context(), "corr-unknown", start); found {
		t.Fatal("unknown correlation id must not match")
	}
}

func TestGetRunETag(t *testing.T) {
	f := newFixture(t)
	res, _ := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-aaaaaaaaaa", "main"))
	run, etag, notModified, err := f.c.GetRun(t.Context(), res.RunID, "")
	if err != nil || notModified || etag == "" || run.Status != "queued" {
		t.Fatalf("first GetRun: %+v %q %v %v", run, etag, notModified, err)
	}
	if _, _, nm, err := f.c.GetRun(t.Context(), res.RunID, etag); err != nil || !nm {
		t.Fatalf("repeat with etag: notModified=%v err=%v, want 304 handled", nm, err)
	}
	f.fake.AdvanceRun(res.RunID)
	run, newTag, nm, err := f.c.GetRun(t.Context(), res.RunID, etag)
	if err != nil || nm || newTag == etag || run.Status != "in_progress" {
		t.Fatalf("after advance: %+v %q %v %v", run, newTag, nm, err)
	}
}

func TestListJobs(t *testing.T) {
	f := newFixture(t)
	res, _ := f.c.Dispatch(t.Context(), dispatchInput(t, f, "pv-aaaaaaaaaa", "main"))
	f.fake.AdvanceRun(res.RunID)
	f.fake.AdvanceRun(res.RunID)
	jobs, err := f.c.ListJobs(t.Context(), res.RunID)
	if err != nil || len(jobs) != 1 || jobs[0].Name != "provision" || len(jobs[0].Steps) != 3 {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	if jobs[0].StartedAt.IsZero() || jobs[0].Steps[2].CompletedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", jobs[0])
	}
}

func TestDispatchRejectedMapsToSentinel(t *testing.T) {
	f := newFixture(t)
	in := dispatchInput(t, f, "pv-aaaaaaaaaa", "main")
	f.c.extraInputs = map[string]any{}
	for i := 0; i < 14; i++ { // 12 + 14 = 26 > 25
		f.c.extraInputs[fmt.Sprintf("x%d", i)] = "v"
	}
	if _, err := f.c.Dispatch(t.Context(), in); !errors.Is(err, ErrDispatchRejected) {
		t.Fatalf("26 inputs: %v, want ErrDispatchRejected", err)
	}
}
