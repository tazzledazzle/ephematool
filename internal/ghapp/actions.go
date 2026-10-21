package ghapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
)

func decodeJSON(r io.Reader, v any) error { return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v) }

// DispatchInput is everything the provisioning workflow receives. The workflow
// has exactly 12 string inputs (GitHub allows 25): the shell never sees them
// except through env: and the first workflow step regex-validates each one.
type DispatchInput struct {
	EnvID, CorrelationID, Traceparent, Repo, SHA, Image string
	Template, ChartVersion, Size, ValuesB64, Owner      string
	ExpiresAt                                           int64 // unix seconds
}

// DispatchResult is the outcome of a dispatch. Ambiguous means GitHub accepted
// the request but returned no run id (HTTP 204): the caller must find the run
// by correlation id and must NOT dispatch again.
type DispatchResult struct {
	RunID     int64
	HTMLURL   string
	Ambiguous bool
}

// Dispatch starts the create-environment workflow on the environments repo.
// go-github pins API version 2022-11-28, under which run details are returned
// only when return_run_details is set; both shapes are handled.
func (c *Client) Dispatch(ctx context.Context, in DispatchInput) (DispatchResult, error) {
	inputs := map[string]any{
		"env_id": in.EnvID, "correlation_id": in.CorrelationID, "traceparent": in.Traceparent,
		"repo": in.Repo, "sha": in.SHA, "image": in.Image, "template": in.Template,
		"chart_version": in.ChartVersion, "size": in.Size, "values_b64": in.ValuesB64,
		"owner": in.Owner, "expires_at": strconv.FormatInt(in.ExpiresAt, 10),
	}
	for k, v := range c.extraInputs { // test hook: lets a test exceed GitHub's 25-input limit
		inputs[k] = v
	}
	rd, resp, err := c.gh.Actions.CreateWorkflowDispatchEventByFileName(ctx, c.owner, c.repo,
		c.cfg.EnvironmentsWorkflow,
		github.CreateWorkflowDispatchEventRequest{
			Ref:              c.cfg.EnvironmentsRef,
			ReturnRunDetails: github.Ptr(true),
			Inputs:           inputs,
		})
	status := statusOf(resp, err)
	if status == http.StatusUnprocessableEntity || status == http.StatusNotFound {
		return DispatchResult{}, fmt.Errorf("%w: HTTP %d", ErrDispatchRejected, status)
	}
	if err != nil {
		return DispatchResult{}, err
	}
	if rd != nil && rd.WorkflowRunID != nil && *rd.WorkflowRunID != 0 {
		return DispatchResult{RunID: *rd.WorkflowRunID, HTMLURL: rd.GetHTMLURL()}, nil
	}
	return DispatchResult{Ambiguous: true}, nil
}

// Run is the subset of a workflow run the platform uses.
type Run struct {
	ID                                        int64
	Status, Conclusion, HTMLURL, DisplayTitle string
	CreatedAt, RunStartedAt, UpdatedAt        time.Time
}

func toRun(r *github.WorkflowRun) Run {
	return Run{
		ID: r.GetID(), Status: r.GetStatus(), Conclusion: r.GetConclusion(),
		HTMLURL: r.GetHTMLURL(), DisplayTitle: r.GetDisplayTitle(),
		CreatedAt: r.GetCreatedAt().Time, RunStartedAt: r.GetRunStartedAt().Time, UpdatedAt: r.GetUpdatedAt().Time,
	}
}

// FindRunByCorrelation lists recent workflow_dispatch runs and returns the one
// whose title (run-name: "create <env_id> <correlation_id>") contains the id.
func (c *Client) FindRunByCorrelation(ctx context.Context, correlationID string, since time.Time) (Run, bool, error) {
	runs, _, err := c.gh.Actions.ListRepositoryWorkflowRuns(ctx, c.owner, c.repo, &github.ListWorkflowRunsOptions{
		Event:       "workflow_dispatch",
		Created:     ">=" + since.Add(-time.Minute).UTC().Format(time.RFC3339),
		ListOptions: github.ListOptions{PerPage: 50},
	})
	if err != nil {
		return Run{}, false, err
	}
	for _, r := range runs.WorkflowRuns {
		if strings.Contains(r.GetDisplayTitle(), correlationID) {
			return toRun(r), true, nil
		}
	}
	return Run{}, false, nil
}

// GetRun fetches a run with an If-None-Match conditional request. When GitHub
// answers 304 the run is unchanged (notModified) and does not count against the
// primary rate limit. go-github has no per-call header option, so the request
// is built by hand.
func (c *Client) GetRun(ctx context.Context, runID int64, etag string) (run Run, newETag string, notModified bool, err error) {
	req, err := c.gh.NewRequest(http.MethodGet,
		fmt.Sprintf("repos/%s/%s/actions/runs/%d", c.owner, c.repo, runID), nil)
	if err != nil {
		return Run{}, "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	var wr github.WorkflowRun
	resp, err := c.gh.Do(ctx, req, &wr)
	if statusOf(resp, err) == http.StatusNotModified {
		return Run{}, etag, true, nil
	}
	if err != nil {
		return Run{}, "", false, err
	}
	return toRun(&wr), resp.Header.Get("ETag"), false, nil
}

// Step and Job mirror the Actions job/step timings used for synthetic spans.
type Step struct {
	Name, Conclusion       string
	Number                 int64
	StartedAt, CompletedAt time.Time
}

type Job struct {
	Name, Conclusion       string
	StartedAt, CompletedAt time.Time
	Steps                  []Step
}

// ListJobs returns the run's jobs with step timings (1-second resolution).
func (c *Client) ListJobs(ctx context.Context, runID int64) ([]Job, error) {
	jobs, _, err := c.gh.Actions.ListWorkflowJobs(ctx, c.owner, c.repo, runID,
		&github.ListWorkflowJobsOptions{ListOptions: github.ListOptions{PerPage: 50}})
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(jobs.Jobs))
	for _, j := range jobs.Jobs {
		job := Job{Name: j.GetName(), Conclusion: j.GetConclusion(),
			StartedAt: j.GetStartedAt().Time, CompletedAt: j.GetCompletedAt().Time}
		for _, s := range j.Steps {
			job.Steps = append(job.Steps, Step{Name: s.GetName(), Conclusion: s.GetConclusion(), Number: s.GetNumber(),
				StartedAt: s.GetStartedAt().Time, CompletedAt: s.GetCompletedAt().Time})
		}
		out = append(out, job)
	}
	return out, nil
}
