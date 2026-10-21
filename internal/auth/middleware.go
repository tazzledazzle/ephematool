package auth

import (
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
)

// PublicExtension is the OpenAPI extension that opts an operation out of the
// session requirement. Everything else is protected by default (WR-02).
const PublicExtension = "x-public"

// MarkPublic exempts an operation from RequireSession. Use it sparingly; the
// httpapi security test lists every public operation.
func MarkPublic(op *huma.Operation) {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[PublicExtension] = true
}

func isPublic(op *huma.Operation) bool {
	v, _ := op.Extensions[PublicExtension].(bool)
	return v
}

// RequireSession is secure by default: every operation answers 401 without a
// signed-in session unless it is marked public. A handler that forgets
// Security can no longer ship unauthenticated.
func RequireSession(api huma.API, sm *scs.SessionManager) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op != nil && isPublic(op) {
			next(ctx)
			return
		}
		if sm == nil || sm.GetInt64(ctx.Context(), KeyUserID) == 0 {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		next(ctx)
	}
}

// RequireRole is a per-operation middleware (huma.Operation.Middlewares). It
// resolves the caller's role NOW, not from the session, so a demotion applies
// to the very next request. Below min, or on a lookup failure, it answers 403
// with the generic "access denied" (no detail about which check failed).
func RequireRole(api huma.API, sm *scs.SessionManager, r Resolver, min string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		p, ok := PrincipalFrom(ctx.Context(), sm)
		if !ok {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		role, err := r.Resolve(ctx.Context(), p.Login)
		if err != nil {
			slog.WarnContext(ctx.Context(), "auth.role_check_failed", "login", p.Login, "error", err.Error())
		}
		if err != nil || !RoleAtLeast(role, min) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "access denied")
			return
		}
		next(ctx)
	}
}
