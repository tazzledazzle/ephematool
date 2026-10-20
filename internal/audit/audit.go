// Package audit records and pages the append-only audit log (D-19). Every
// create attempt is recorded: success, denied (not allowed to try) or rejected
// (allowed but the request was invalid). Reasons are stable machine codes; the
// UI maps them to copy. Never store tokens or the traceparent header here.
package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/store"
)

// Actions and outcomes.
const (
	ActionEnvironmentCreate = "environment.create"

	OutcomeSuccess  = "success"
	OutcomeDenied   = "denied"
	OutcomeRejected = "rejected"
)

// Reason codes (stored, stable). The UI owns the human copy.
const (
	ReasonNotCreator        = "not_creator"
	ReasonRepoNotAllowed    = "repo_not_allowed"
	ReasonRefNotFound       = "ref_not_found"
	ReasonRefInvalid        = "ref_invalid"
	ReasonPRNotEligible     = "pr_not_eligible"
	ReasonImageNotFound     = "image_not_found"
	ReasonSchemaViolation   = "schema_violation"
	ReasonTemplateRequired  = "template_required"
	ReasonSizeRequired      = "size_required"
	ReasonGitHubUnavailable = "github_unavailable"
)

// Event is one audit entry to write.
type Event struct {
	ActorGithubID int64
	ActorLogin    string
	Action        string
	EnvironmentID string // empty when no environment exists (denied/rejected)
	Outcome       string
	Reason        string
	TraceID       string // filled from ctx when empty
}

// Record appends an event. Pass a transaction-bound *store.Queries to make the
// audit row commit atomically with the change it describes.
func Record(ctx context.Context, q *store.Queries, e Event) error {
	e.TraceID = traceIDFrom(ctx, e.TraceID)
	_, err := q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ActorGithubID: e.ActorGithubID,
		ActorLogin:    e.ActorLogin,
		Action:        e.Action,
		EnvironmentID: pgtype.Text{String: e.EnvironmentID, Valid: e.EnvironmentID != ""},
		Outcome:       e.Outcome,
		Reason:        e.Reason,
		TraceID:       e.TraceID,
	})
	if err != nil {
		return fmt.Errorf("audit insert: %w", err)
	}
	return nil
}

// traceIDFrom returns explicit when set, else the trace id of the span in ctx.
func traceIDFrom(ctx context.Context, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// Filter narrows List. Empty fields mean "no filter".
type Filter struct {
	Outcome, Action, Actor, Cursor string
	Limit                          int // default 50, max 100
}

// Item is one audit row as returned to callers.
type Item struct {
	ID             string
	OccurredAt     time.Time
	ActorGithubID  int64
	ActorLogin     string
	ActorAvatarURL string
	Action         string
	EnvironmentID  string
	Outcome        string
	Reason         string
	TraceID        string
}

// Page is one page of items, newest first.
type Page struct {
	Events     []Item
	NextCursor string // empty on the last page
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// List returns one page, newest first, with an opaque keyset cursor.
func List(ctx context.Context, q *store.Queries, f Filter) (Page, error) {
	limit := normalizeLimit(f.Limit)
	p := store.ListAuditEventsParams{
		Outcome: text(f.Outcome), Action: text(f.Action), Actor: text(f.Actor),
		// Fetch one extra row to learn whether another page exists.
		RowLimit: int32(limit + 1),
	}
	if f.Cursor != "" {
		at, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return Page{}, err
		}
		p.CursorAt = pgtype.Timestamptz{Time: at, Valid: true}
		p.CursorID = pgtype.UUID{Bytes: id, Valid: true}
	}
	rows, err := q.ListAuditEvents(ctx, p)
	if err != nil {
		return Page{}, fmt.Errorf("audit list: %w", err)
	}
	var page Page
	for i, r := range rows {
		if i == limit { // the extra row only proves there is a next page
			last := rows[limit-1]
			page.NextCursor = encodeCursor(last.OccurredAt.Time, last.ID.Bytes)
			break
		}
		page.Events = append(page.Events, Item{
			ID: uuidString(r.ID.Bytes), OccurredAt: r.OccurredAt.Time,
			ActorGithubID: r.ActorGithubID, ActorLogin: r.ActorLogin, ActorAvatarURL: r.ActorAvatarUrl,
			Action: r.Action, EnvironmentID: r.EnvironmentID.String, Outcome: r.Outcome,
			Reason: r.Reason, TraceID: r.TraceID,
		})
	}
	if page.Events == nil {
		page.Events = []Item{}
	}
	return page, nil
}
