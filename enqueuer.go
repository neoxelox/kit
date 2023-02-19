package kit

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/hibiken/asynq"
)

const (
	_ENQUEUER_REDIS_DSN = "%s:%d"
)

var (
	_ENQUEUER_DEFAULT_MAX_CONNS     = 10 * runtime.GOMAXPROCS(-1)
	_ENQUEUER_DEFAULT_READ_TIMEOUT  = 30 * time.Second
	_ENQUEUER_DEFAULT_WRITE_TIMEOUT = 30 * time.Second
	_ENQUEUER_DEFAULT_DIAL_TIMEOUT  = 30 * time.Second
)

type EnqueuerConfig struct {
	CacheHost         string
	CachePort         int
	CachePassword     string
	CacheMaxConns     *int
	CacheReadTimeout  *time.Duration
	CacheWriteTimeout *time.Duration
	CacheDialTimeout  *time.Duration
}

type Enqueuer struct {
	config   EnqueuerConfig
	observer Observer
	client   asynq.Client
}

func NewEnqueuer(observer Observer, config EnqueuerConfig) *Enqueuer {
	if config.CacheMaxConns == nil {
		config.CacheMaxConns = ptr(_ENQUEUER_DEFAULT_MAX_CONNS)
	}

	if config.CacheReadTimeout == nil {
		config.CacheReadTimeout = ptr(_ENQUEUER_DEFAULT_READ_TIMEOUT)
	}

	if config.CacheWriteTimeout == nil {
		config.CacheWriteTimeout = ptr(_ENQUEUER_DEFAULT_WRITE_TIMEOUT)
	}

	if config.CacheDialTimeout == nil {
		config.CacheDialTimeout = ptr(_ENQUEUER_DEFAULT_DIAL_TIMEOUT)
	}

	dsn := fmt.Sprintf(_ENQUEUER_REDIS_DSN, config.CacheHost, config.CachePort)

	redisConfig := asynq.RedisClientOpt{
		Addr:         dsn,
		Password:     config.CachePassword,
		DialTimeout:  *config.CacheDialTimeout,
		ReadTimeout:  *config.CacheReadTimeout,
		WriteTimeout: *config.CacheWriteTimeout,
		PoolSize:     *config.CacheMaxConns,
	}

	return &Enqueuer{
		config:   config,
		observer: observer,
		client:   *asynq.NewClient(redisConfig),
	}
}

func (self *Enqueuer) Enqueue(ctx context.Context, task string, params interface{}, options ...asynq.Option) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return ErrEnqueuerGeneric().Wrap(err)
	}

	info, err := self.client.EnqueueContext(ctx, asynq.NewTask(task, payload), options...)
	if err != nil {
		return ErrEnqueuerGeneric().Wrap(err)
	}

	self.observer.Infof(ctx, "Enqueued task %s on queue %s with id %s", info.Type, info.Queue, info.ID)

	return nil
}

func (self *Enqueuer) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info(ctx, "Closing enqueuer")

		err := self.client.Close()
		if err != nil {
			return ErrEnqueuerGeneric().WrapAs(err)
		}

		self.observer.Info(ctx, "Closed enqueuer")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrEnqueuerTimedOut()
	default:
		return ErrEnqueuerGeneric().Wrap(err)
	}
}
