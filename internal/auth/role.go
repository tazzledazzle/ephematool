package auth

import "strings"

// RoleFor returns "admin" when login is in adminLogins (case-insensitive, D-02)
// and "viewer" otherwise. The role is evaluated at login and stored in the
// session, so a change to ADMIN_LOGINS applies at the next sign-in (assumption
// A3). Admin pinning by numeric GitHub id is a Phase 2 hardening item.
func RoleFor(login string, adminLogins []string) string {
	for _, a := range adminLogins {
		if strings.EqualFold(strings.TrimSpace(a), login) {
			return "admin"
		}
	}
	return "viewer"
}
