package main

//go:generate sh -c "go run . openapi > ../../api/openapi.json"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/httpapi"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/status"
	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/internal/telemetry"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	case "migrate":
		if err := migrate(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	case "openapi":
		if err := printOpenAPI(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (serve|migrate|openapi)\n", cmd)
		os.Exit(2)
	}
}

func printOpenAPI() error {
	_, api := httpapi.New(httpapi.Deps{})
	b, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, buf.String())
	return err
}

func migrate() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return store.Migrate(ctx, pool)
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "ephematool-api",
		Version:     buildinfo.Version,
		Env:         cfg.AppEnv,
	})
	if err != nil {
		return err
	}
	defer func() {
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdownTelemetry(fctx)
	}()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := redisotel.InstrumentTracing(rdb); err != nil {
		return err
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return err
	}
	defer rdb.Close()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if cfg.MigrateOnStart {
		if err := store.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	queries := store.New(pool)
	sessions := auth.NewSessionManager(rdb, cfg)

	q := queue.NewClient(cfg.RedisAddr)
	defer q.Close()

	prober, closeProbe, err := newProber(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeProbe()
	go prober.Run(ctx)

	handler, _ := httpapi.New(httpapi.Deps{
		Config: cfg, Redis: rdb, Queue: q, Status: prober,
		Sessions: sessions, Queries: queries, Auth: auth.NewHandlers(cfg, sessions, queries),
	})
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "api.listening", "addr", cfg.HTTPAddr, "version", buildinfo.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.InfoContext(sctx, "api.shutting_down")
		return srv.Shutdown(sctx)
	}
	return nil
}

// newProber builds the status prober. It owns uninstrumented Postgres and Redis
// connections so the probes do not create a trace every cycle.
func newProber(ctx context.Context, cfg config.Config) (*status.Prober, func(), error) {
	probePool, err := store.OpenUntraced(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open probe pool: %w", err)
	}
	probeRedis := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})

	checks := status.Checks{
		Postgres: func(c context.Context) error { return probePool.Ping(c) },
		Redis:    func(c context.Context) error { return probeRedis.Ping(c).Err() },
		HeartbeatAt: func(c context.Context) (time.Time, error) {
			v, err := probeRedis.Get(c, queue.HeartbeatKey).Result()
			if err != nil {
				return time.Time{}, err
			}
			secs, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(secs, 0), nil
		},
		HTTP: map[string]string{
			"dashboard":      cfg.WebHealthURL,
			"otel-collector": cfg.CollectorHealthURL,
			"tempo":          cfg.TempoReadyURL,
			"loki":           cfg.LokiReadyURL,
			"prometheus":     cfg.PrometheusReadyURL,
			"grafana":        cfg.GrafanaHealthURL,
		},
		LastSignal: status.NewLastSignal(cfg.PrometheusURL, cfg.ProbeTimeout),
	}
	p := status.NewProber(checks, cfg.ProbeInterval, cfg.ProbeTimeout, otel.Meter("ephematool/status"))
	return p, func() { probePool.Close(); _ = probeRedis.Close() }, nil
}
