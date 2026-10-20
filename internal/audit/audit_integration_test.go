//go:build integration

package audit_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ephematool/ephematool/internal/audit"
	"github.com/ephematool/ephematool/internal/testenv"
)

// uniqueActor isolates this test's rows: audit_events can never be truncated.
func uniqueActor(t *testing.T) (int64, string) {
	n := time.Now().UnixNano()
	return n % 1e12, fmt.Sprintf("actor-%d", n)
}

func TestRecordAndList(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	id, login := uniqueActor(t)

	events := []audit.Event{
		{Outcome: audit.OutcomeSuccess, Reason: "", EnvironmentID: "pv-aaaaaaaaaa"},
		{Outcome: audit.OutcomeDenied, Reason: audit.ReasonNotCreator},
		{Outcome: audit.OutcomeRejected, Reason: audit.ReasonRefNotFound},
		{Outcome: audit.OutcomeDenied, Reason: audit.ReasonRepoNotAllowed},
		{Outcome: audit.OutcomeSuccess, EnvironmentID: "pv-bbbbbbbbbb"},
	}
	for _, e := range events {
		e.ActorGithubID, e.ActorLogin, e.Action, e.TraceID = id, login, audit.ActionEnvironmentCreate, "0123456789abcdef0123456789abcdef"
		if err := audit.Record(ctx, env.Queries, e); err != nil {
			t.Fatal(err)
		}
	}

	all, err := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Limit: 50})
	if err != nil || len(all.Events) != 5 || all.NextCursor != "" {
		t.Fatalf("List = %d events cursor %q err %v, want 5 and no cursor", len(all.Events), all.NextCursor, err)
	}
	if all.Events[0].EnvironmentID != "pv-bbbbbbbbbb" || all.Events[0].TraceID == "" {
		t.Errorf("newest first with trace id expected, got %+v", all.Events[0])
	}
	denied, _ := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Outcome: audit.OutcomeDenied})
	if len(denied.Events) != 2 {
		t.Errorf("outcome=denied returned %d, want 2", len(denied.Events))
	}
	none, _ := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Action: "nope"})
	if len(none.Events) != 0 {
		t.Errorf("unknown action returned %d, want 0", len(none.Events))
	}

	// Paging: 5 events, limit 2 -> 2, 2, 1 with an empty cursor at the end.
	var sizes []int
	cursor := ""
	for i := 0; i < 5; i++ {
		page, err := audit.List(ctx, env.Queries, audit.Filter{Actor: login, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(page.Events))
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if fmt.Sprint(sizes) != "[2 2 1]" {
		t.Errorf("page sizes = %v, want [2 2 1]", sizes)
	}
	if _, err := audit.List(ctx, env.Queries, audit.Filter{Cursor: "garbage"}); err == nil {
		t.Error("garbage cursor must return ErrBadCursor")
	}
}

func TestAvatarFromUsers(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	id, login := uniqueActor(t)
	if _, err := env.App.Exec(ctx, "insert into users (github_id, login, avatar_url) values ($1, $2, 'http://a/b.png')", id, login); err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(ctx, env.Queries, audit.Event{ActorGithubID: id, ActorLogin: login,
		Action: audit.ActionEnvironmentCreate, Outcome: audit.OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	page, _ := audit.List(ctx, env.Queries, audit.Filter{Actor: login})
	if len(page.Events) != 1 || page.Events[0].ActorAvatarURL != "http://a/b.png" {
		t.Fatalf("avatar not joined from users: %+v", page.Events)
	}
}
