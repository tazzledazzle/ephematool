package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// Heartbeat key written by the worker; the status endpoint reads it.
const (
	HeartbeatKey = "worker:heartbeat"
	HeartbeatTTL = 45 * time.Second
)

// heartbeatHandler writes the current unix time to HeartbeatKey with a TTL.
func heartbeatHandler(rdb *redis.Client) asynq.HandlerFunc {
	return func(ctx context.Context, _ *asynq.Task) error {
		if err := rdb.Set(ctx, HeartbeatKey, time.Now().Unix(), HeartbeatTTL).Err(); err != nil {
			return err
		}
		slog.InfoContext(ctx, "worker heartbeat")
		return nil
	}
}

func pingHandler(ctx context.Context, _ *asynq.Task) error {
	slog.InfoContext(ctx, "ping handled")
	return nil
}
