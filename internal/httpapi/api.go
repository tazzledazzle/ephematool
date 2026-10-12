// Package httpapi wires the Huma API, route-accurate telemetry and handlers.
package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/queue"
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

// New builds the HTTP handler and the Huma API. Plan 01-04 inserts the session
// and CrossOriginProtection layers here.
func New(deps Deps) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Ephematool API", buildinfo.Version))
	api.UseMiddleware(RouteTagging)
	registerStatus(api, deps)
	return otelhttp.NewHandler(TraceIDHeader(mux), "ephematool-api"), api
}
