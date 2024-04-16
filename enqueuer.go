package kit

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hibiken/asynq"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

const (
	_ENQUEUER_REDIS_DSN            = "%s:%d"
	_ENQUEUER_TASK_TRACE_ID_HEADER = "x_trace_id"
)

var (
	ErrEnqueuerGeneric  = errors.New("enqueuer failed")
	ErrEnqueuerTimedOut = errors.New("enqueuer timed out")
)

var (
	_ENQUEUER_DEFAULT_CONFIG = EnqueuerConfig{
		CacheMaxConns:     util.Pointer(max(8, 4*runtime.GOMAXPROCS(-1))),
		CacheReadTimeout:  util.Pointer(30 * time.Second),
		CacheWriteTimeout: util.Pointer(30 * time.Second),
		CacheDialTimeout:  util.Pointer(30 * time.Second),
		TaskDefaultRetry:  util.Pointer(0),
	}
)

type EnqueuerConfig struct {
	CacheHost         string
	CachePort         int
	CacheSSLMode      bool
	CachePassword     string
	CacheMaxConns     *int
	CacheReadTimeout  *time.Duration
	CacheWriteTimeout *time.Duration
	CacheDialTimeout  *time.Duration
	TaskDefaultRetry  *int
}

type Enqueuer struct {
	config   EnqueuerConfig
	observer *Observer
	client   *asynq.Client
}

func NewEnqueuer(observer *Observer, config EnqueuerConfig) *Enqueuer {
	util.Merge(&config, _ENQUEUER_DEFAULT_CONFIG)

	dsn := fmt.Sprintf(_ENQUEUER_REDIS_DSN, config.CacheHost, config.CachePort)

	var ssl *tls.Config
	if config.CacheSSLMode {
		ssl = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	redisConfig := asynq.RedisClientOpt{
		Addr:         dsn,
		TLSConfig:    ssl,
		Password:     config.CachePassword,
		DialTimeout:  *config.CacheDialTimeout,
		ReadTimeout:  *config.CacheReadTimeout,
		WriteTimeout: *config.CacheWriteTimeout,
		PoolSize:     *config.CacheMaxConns,
	}

	return &Enqueuer{
		config:   config,
		observer: observer,
		client:   asynq.NewClient(redisConfig),
	}
}

func (self *Enqueuer) Enqueue(ctx context.Context, task string, params any, options ...asynq.Option) error {
	traceID := self.observer.GetTrace(ctx)
	sentrySpan := sentry.SpanFromContext(ctx)

	payload, err := json.Marshal(params)
	if err != nil {
		return ErrEnqueuerGeneric.Raise().Cause(err)
	}

	var data map[string]any

	err = json.Unmarshal(payload, &data)
	if err != nil {
		return ErrEnqueuerGeneric.Raise().Cause(err)
	}

	data[_ENQUEUER_TASK_TRACE_ID_HEADER] = traceID
	if sentrySpan != nil {
		data[sentry.SentryTraceHeader] = sentrySpan.ToSentryTrace()
	}

	payload, err = json.Marshal(data)
	if err != nil {
		return ErrEnqueuerGeneric.Raise().Cause(err)
	}

	info, err := self.client.EnqueueContext(ctx,
		asynq.NewTask(task, payload, asynq.MaxRetry(*self.config.TaskDefaultRetry)), options...)
	if err != nil {
		return ErrEnqueuerGeneric.Raise().Cause(err)
	}

	self.observer.Infof(
		ctx, "Enqueued task %s on queue %s with id %s and trace %s", info.Type, info.Queue, info.ID, traceID)

	return nil
}

func (self *Enqueuer) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info(ctx, "Closing enqueuer")

		err := self.client.Close()
		if err != nil {
			return ErrEnqueuerGeneric.Raise().Cause(err)
		}

		self.observer.Info(ctx, "Closed enqueuer")

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrEnqueuerTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}
