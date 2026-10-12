package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-github/v92/github"
)

// Decision is the outcome of an allowlist evaluation (D-01, D-04).
type Decision struct {
	Allowed bool
	// Reason is "org_membership" when denied.
	Reason string
	// GrantedBy lists the orgs ("org") and teams ("org/slug") that allowed the user.
	GrantedBy []string
	// Forbidden is true when an org check returned 403 (OAuth app access restrictions).
	Forbidden bool
}

// MembershipClient abstracts the GitHub membership lookups (faked in tests).
type MembershipClient interface {
	OrgMembership(ctx context.Context, org string) (state string, status int, err error)
	TeamMembership(ctx context.Context, org, slug, login string) (state string, status int, err error)
}

// Allowlist is the configured set of orgs and "org/slug" teams.
type Allowlist struct {
	Orgs  []string
	Teams []string
}

const stateActive = "active"

// Evaluate fails closed: only an "active" membership in at least one
// configured org or team allows the login. Pending, 404, 403, transport
// errors and an empty allowlist all deny.
func (a Allowlist) Evaluate(ctx context.Context, mc MembershipClient, login string) Decision {
	d := Decision{Reason: "org_membership"}
	for _, org := range a.Orgs {
		state, status, err := mc.OrgMembership(ctx, org)
		if status == http.StatusForbidden {
			d.Forbidden = true
			slog.WarnContext(ctx, "auth.org_check_forbidden", "org", org, "login", login)
			continue
		}
		if err == nil && status == http.StatusOK && state == stateActive {
			d.GrantedBy = append(d.GrantedBy, org)
		}
	}
	for _, team := range a.Teams {
		org, slug, ok := strings.Cut(team, "/")
		if !ok || org == "" || slug == "" {
			continue
		}
		state, status, err := mc.TeamMembership(ctx, org, slug, login)
		if err == nil && status == http.StatusOK && state == stateActive {
			d.GrantedBy = append(d.GrantedBy, team)
		}
	}
	if len(d.GrantedBy) > 0 {
		d.Allowed = true
		d.Reason = ""
		d.Forbidden = false
	}
	return d
}

// GitHubMembershipClient implements MembershipClient with go-github, acting as
// the signed-in user (the access token lives only inside the client).
type GitHubMembershipClient struct {
	Client *github.Client
}

func statusOf(resp *github.Response, err error) int {
	if resp != nil && resp.Response != nil {
		return resp.StatusCode
	}
	var er *github.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		return er.Response.StatusCode
	}
	return 0
}

func (g GitHubMembershipClient) OrgMembership(ctx context.Context, org string) (string, int, error) {
	m, resp, err := g.Client.Organizations.GetOrgMembership(ctx, "", org)
	status := statusOf(resp, err)
	if err != nil || m == nil {
		return "", status, err
	}
	return m.GetState(), status, nil
}

func (g GitHubMembershipClient) TeamMembership(ctx context.Context, org, slug, login string) (string, int, error) {
	m, resp, err := g.Client.Teams.GetTeamMembershipBySlug(ctx, org, slug, login)
	status := statusOf(resp, err)
	if err != nil || m == nil {
		return "", status, err
	}
	return m.GetState(), status, nil
}
