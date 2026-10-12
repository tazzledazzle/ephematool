package queue

import (
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// NewServer builds the asynq consumer. It uses a plain RedisClientOpt, so its
// polling traffic is never traced (Pitfall 4).
func NewServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 4,
		Queues:      map[string]int{"default": 1},
	})
}

// NewMux registers the task handlers wrapped in TracingMiddleware. rdb is the
// instrumented application client used by the heartbeat.
func NewMux(rdb *redis.Client) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(TracingMiddleware)
	mux.HandleFunc(TypePing, pingHandler)
	mux.HandleFunc(TypeHeartbeat, heartbeatHandler(rdb))
	return mux
}

// NewScheduler registers the periodic heartbeat task.
func NewScheduler(redisAddr string, every time.Duration) (*asynq.Scheduler, error) {
	s := asynq.NewScheduler(asynq.RedisClientOpt{Addr: redisAddr}, nil)
	uniq := every - time.Second
	if uniq < time.Second {
		uniq = time.Second
	}
	if _, err := s.Register("@every "+every.String(), asynq.NewTask(TypeHeartbeat, nil),
		asynq.Unique(uniq), asynq.MaxRetry(0)); err != nil {
		return nil, err
	}
	return s, nil
}
