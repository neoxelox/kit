package kit

import (
	"context"
	"crypto/tls"
	"fmt"
	"runtime"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

const (
	_LIMITER_REDIS_DSN = "%s:%d"
)

var (
	KeyLimiter Key = KeyBase + "limiter:"
)

var (
	ErrLimiterGeneric  = errors.New("limiter failed")
	ErrLimiterTimedOut = errors.New("limiter timed out")
)

var (
	_LIMITER_DEFAULT_CONFIG = LimiterConfig{
		CacheMinConns:        util.Pointer(1),
		CacheMaxConns:        util.Pointer(max(8, 4*runtime.GOMAXPROCS(-1))),
		CacheMaxConnIdleTime: util.Pointer(30 * time.Minute),
		CacheMaxConnLifeTime: util.Pointer(1 * time.Hour),
		CacheReadTimeout:     util.Pointer(30 * time.Second),
		CacheWriteTimeout:    util.Pointer(30 * time.Second),
		CacheDialTimeout:     util.Pointer(30 * time.Second),
	}
)

type LimiterConfig struct {
	CacheHost            string
	CachePort            int
	CacheSSLMode         bool
	CachePassword        string
	CacheMinConns        *int
	CacheMaxConns        *int
	CacheMaxConnIdleTime *time.Duration
	CacheMaxConnLifeTime *time.Duration
	CacheReadTimeout     *time.Duration
	CacheWriteTimeout    *time.Duration
	CacheDialTimeout     *time.Duration
}

type Limiter struct {
	config   LimiterConfig
	observer *Observer
	client   *redis.Client
}

func NewLimiter(observer *Observer, config LimiterConfig) *Limiter {
	util.Merge(&config, _LIMITER_DEFAULT_CONFIG)

	redis.SetLogger(_newRedisLogger(observer))

	dsn := fmt.Sprintf(_LIMITER_REDIS_DSN, config.CacheHost, config.CachePort)

	var ssl *tls.Config
	if config.CacheSSLMode {
		ssl = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	redisConfig := &redis.Options{
		Addr:         dsn,
		TLSConfig:    ssl,
		Password:     config.CachePassword,
		MinIdleConns: *config.CacheMinConns,
		PoolSize:     *config.CacheMaxConns,
		IdleTimeout:  *config.CacheMaxConnIdleTime,
		MaxConnAge:   *config.CacheMaxConnLifeTime,
		ReadTimeout:  *config.CacheReadTimeout,
		WriteTimeout: *config.CacheWriteTimeout,
		DialTimeout:  *config.CacheDialTimeout,
		PoolTimeout:  *config.CacheDialTimeout,
	}

	return &Limiter{
		config:   config,
		observer: observer,
		client:   redis.NewClient(redisConfig),
	}
}

func (self *Limiter) Limit(ctx context.Context, key string, limit int, period time.Duration) (int, error) {
	key = string(KeyLimiter) + key

	pipeline := self.client.TxPipeline()

	increment := pipeline.Incr(ctx, key)
	pipeline.Expire(ctx, key, period)

	_, err := pipeline.Exec(ctx)
	if err != nil {
		return -1, ErrLimiterGeneric.Raise().Cause(err)
	}

	rate, err := increment.Result()
	if err != nil {
		return -1, ErrLimiterGeneric.Raise().Cause(err)
	}

	if rate > int64(limit) {
		return -1, nil
	}

	return int(int64(limit) - rate), nil
}

func (self *Limiter) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info(ctx, "Closing limiter")

		err := self.client.Close()
		if err != nil {
			return ErrLimiterGeneric.Raise().Cause(err)
		}

		self.observer.Info(ctx, "Closed limiter")

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrLimiterTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}
