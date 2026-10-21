//go:build integration

package auth_test

import (
	"testing"

	"github.com/ephematool/ephematool/internal/auth/authtest"
	"github.com/ephematool/ephematool/internal/config"
)

// A user in the creators team signs in and is a creator (the role hint comes
// from the user's own token because no GitHub App checker is configured here).
func TestCreatorTeamRole(t *testing.T) {
	e := authtest.NewEnv(t, func(c *config.Config) {
		c.CreatorTeams = []string{"ephematool-e2e/creators"}
	})
	e.Login(t, "octo-creator", "/")
	if status, me := getMe(t, e); status != 200 || me.Role != "creator" || me.Login != "octo-creator" {
		t.Fatalf("me = %d %+v, want creator", status, me)
	}
}

// WR-10: switching accounts in one browser must not leak the previous identity.
func TestAccountSwitchLeavesNoAdminData(t *testing.T) {
	e := authtest.NewEnv(t)
	e.Login(t, "octo-admin", "/")
	if _, me := getMe(t, e); me.Role != "admin" {
		t.Fatalf("setup: first login should be admin, got %+v", me)
	}
	e.Login(t, "octo-viewer", "/") // same cookie jar
	status, me := getMe(t, e)
	if status != 200 || me.Role != "viewer" || me.Login != "octo-viewer" {
		t.Fatalf("after switching accounts me = %d %+v, want viewer octo-viewer", status, me)
	}
}
