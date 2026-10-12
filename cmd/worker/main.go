// Command worker consumes asynq tasks and emits its own telemetry.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "ephematool-worker",
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

	srv := queue.NewServer(cfg.RedisAddr)
	sched, err := queue.NewScheduler(cfg.RedisAddr, cfg.HeartbeatInterval)
	if err != nil {
		return err
	}
	if err := sched.Start(); err != nil {
		return err
	}
	if err := srv.Start(queue.NewMux(rdb)); err != nil {
		return err
	}
	slog.InfoContext(ctx, "worker started", "version", buildinfo.Version)

	<-ctx.Done()
	slog.InfoContext(context.Background(), "worker shutting down")
	srv.Shutdown()
	sched.Shutdown()
	return nil
}
