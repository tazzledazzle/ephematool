// Package httpapi wires the Huma API, route-accurate telemetry and handlers.
package httpapi

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/store"
)

// Enqueuer publishes the telemetry ping task. *queue.Client satisfies it.
type Enqueuer interface {
	EnqueuePing(ctx context.Context) error
}

// Deps are the collaborators handlers need. Later plans add fields.
type Deps struct {
	Config config.Config
	Redis  *redis.Client
	Queue  *queue.Client
	// Enqueuer overrides Queue when set (tests).
	Enqueuer Enqueuer
	// Sessions, Queries and Auth are optional; Auth is built from the others
	// when nil. All may be nil when only the OpenAPI spec is generated.
	Sessions *scs.SessionManager
	Queries  *store.Queries
	Auth     *auth.Handlers
}

func (d Deps) enqueuer() Enqueuer {
	if d.Enqueuer != nil {
		return d.Enqueuer
	}
	if d.Queue != nil {
		return d.Queue
	}
	return nil
}

// New builds the HTTP handler and the Huma API. Layer order, outermost first:
// CrossOriginProtection -> otelhttp -> TraceIDHeader -> sessions.LoadAndSave -> mux.
func New(deps Deps) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Ephematool API", buildinfo.Version))
	api.UseMiddleware(RouteTagging)
	if deps.Auth == nil {
		deps.Auth = auth.NewHandlers(deps.Config, deps.Sessions, deps.Queries)
	}
	auth.Register(api, mux, deps.Auth, RouteHandler)
	registerStatus(api, deps)

	var inner http.Handler = mux
	if deps.Sessions != nil {
		inner = deps.Sessions.LoadAndSave(mux)
	}
	h := otelhttp.NewHandler(TraceIDHeader(inner), "ephematool-api")
	return http.NewCrossOriginProtection().Handler(h), api
}
