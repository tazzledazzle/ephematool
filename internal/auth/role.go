package auth

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/alexedwards/scs/v2"

	"github.com/ephematool/ephematool/internal/config"
)

// Roles, lowest to highest. A creator can create environments; an admin can do
// that and read the audit log.
const (
	RoleViewer  = "viewer"
	RoleCreator = "creator"
	RoleAdmin   = "admin"
)

// TeamChecker answers "is login an active member of org/slug?". *ghapp.Client
// satisfies it (auth must not import ghapp).
type TeamChecker interface {
	IsTeamMember(ctx context.Context, org, slug, login string) (bool, error)
}

// Resolver turns a login into a role at request time (WR-01).
type Resolver interface {
	Resolve(ctx context.Context, login string) (string, error)
}

// RoleResolver implements D-06: admin from ADMIN_LOGINS, creator from
// CREATOR_LOGINS or membership of a CREATOR_TEAMS team, otherwise viewer.
// Membership is looked up per call with the platform's App token, so removing
// someone from the team takes effect on their next request, not at next login.
type RoleResolver struct {
	AdminLogins, CreatorLogins, CreatorTeams []string
	Teams                                    TeamChecker // nil: team checks are skipped

	warnOnce *sync.Once // logs the "teams unchecked" warning once per resolver
}

// NewRoleResolver builds a resolver from config. teams may be nil.
func NewRoleResolver(cfg config.Config, teams TeamChecker) RoleResolver {
	return RoleResolver{
		AdminLogins: cfg.AdminLogins, CreatorLogins: cfg.CreatorLogins, CreatorTeams: cfg.CreatorTeams,
		Teams: teams, warnOnce: &sync.Once{},
	}
}

func inList(list []string, login string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), login) {
			return true
		}
	}
	return false
}

// Resolve returns the caller's current role. Order: admin, creator login,
// creator team, viewer. A failed team lookup returns viewer plus the error:
// fail closed, and the caller logs auth.role_check_failed.
func (r RoleResolver) Resolve(ctx context.Context, login string) (string, error) {
	if inList(r.AdminLogins, login) {
		return RoleAdmin, nil
	}
	if inList(r.CreatorLogins, login) {
		return RoleCreator, nil
	}
	if len(r.CreatorTeams) == 0 {
		return RoleViewer, nil
	}
	if r.Teams == nil {
		if r.warnOnce != nil {
			r.warnOnce.Do(func() {
				slog.WarnContext(ctx, "auth.creator_teams_unchecked",
					"reason", "GitHub App not configured; CREATOR_TEAMS cannot be evaluated")
			})
		}
		return RoleViewer, nil
	}
	for _, team := range r.CreatorTeams {
		org, slug, ok := strings.Cut(team, "/")
		if !ok {
			continue
		}
		member, err := r.Teams.IsTeamMember(ctx, org, slug, login)
		if err != nil {
			return RoleViewer, err
		}
		if member {
			return RoleCreator, nil
		}
	}
	return RoleViewer, nil
}

var rank = map[string]int{RoleViewer: 1, RoleCreator: 2, RoleAdmin: 3}

// RoleAtLeast reports whether role is min or higher (viewer < creator < admin).
// Unknown roles rank 0 and never satisfy anything.
func RoleAtLeast(role, min string) bool { return rank[role] >= rank[min] && rank[role] > 0 }

// Principal is the signed-in user as stored in the session.
type Principal struct {
	GithubID  int64
	Login     string
	AvatarURL string
}

// PrincipalFrom reads the signed-in user from the session; ok is false when
// nobody is signed in.
func PrincipalFrom(ctx context.Context, sm *scs.SessionManager) (Principal, bool) {
	if sm == nil {
		return Principal{}, false
	}
	id := sm.GetInt64(ctx, KeyUserID)
	if id == 0 {
		return Principal{}, false
	}
	return Principal{GithubID: id, Login: sm.GetString(ctx, KeyLogin), AvatarURL: sm.GetString(ctx, KeyAvatarURL)}, true
}

// userTeamChecker adapts the login-time, user-token MembershipClient so the
// login hint can see creator teams even before the App is configured.
type userTeamChecker struct{ mc MembershipClient }

func (u userTeamChecker) IsTeamMember(ctx context.Context, org, slug, login string) (bool, error) {
	state, status, err := u.mc.TeamMembership(ctx, org, slug, login)
	if status == 404 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == 200 && state == stateActive, nil
}
