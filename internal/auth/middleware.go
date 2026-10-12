package auth

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
)

// RequireSession returns a Huma middleware that answers 401 for operations
// whose Security includes the "session" scheme when no user is signed in.
func RequireSession(api huma.API, sm *scs.SessionManager) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op == nil || !needsSession(op) {
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

func needsSession(op *huma.Operation) bool {
	for _, req := range op.Security {
		if _, ok := req["session"]; ok {
			return true
		}
	}
	return false
}
