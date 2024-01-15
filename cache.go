package kit

import (
	"context"
	"crypto/tls"
	"fmt"
	"runtime"
	"time"

	"github.com/neoxelox/errors"

	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/v8"

	"github.com/neoxelox/kit/util"
)

const (
	_CACHE_REDIS_DSN = "%s:%d"
)

var (
	ErrCacheGeneric   = errors.New("cache failed")
	ErrCacheTimedOut  = errors.New("cache timed out")
	ErrCacheUnhealthy = errors.New("cache unhealthy")
	ErrCacheMiss      = errors.New("cache key not found")
)

var (
	_CACHE_DEFAULT_CONFIG = CacheConfig{
		MinConns:        util.Pointer(1),
		MaxConns:        util.Pointer(max(8, 4*runtime.GOMAXPROCS(-1))),
		MaxConnIdleTime: util.Pointer(30 * time.Minute),
		MaxConnLifeTime: util.Pointer(1 * time.Hour),
		ReadTimeout:     util.Pointer(30 * time.Second),
		WriteTimeout:    util.Pointer(30 * time.Second),
		DialTimeout:     util.Pointer(30 * time.Second),
	}

	_CACHE_DEFAULT_RETRY_CONFIG = RetryConfig{
		Attempts:     1,
		InitialDelay: 0 * time.Second,
		LimitDelay:   0 * time.Second,
		Retriables:   []error{},
	}
)

type CacheConfig struct {
	Host            string
	Port            int
	SSLMode         bool
	Password        string
	MinConns        *int
	MaxConns        *int
	MaxConnIdleTime *time.Duration
	MaxConnLifeTime *time.Duration
	ReadTimeout     *time.Duration
	WriteTimeout    *time.Duration
	DialTimeout     *time.Duration
}

type Cache struct {
	config   CacheConfig
	observer *Observer
	pool     *redis.Client
	cache    *cache.Cache
}

func NewCache(ctx context.Context, observer *Observer, config CacheConfig, retry ...RetryConfig) (*Cache, error) {
	util.Merge(&config, _CACHE_DEFAULT_CONFIG)
	_retry := util.Optional(retry, _CACHE_DEFAULT_RETRY_CONFIG)

	redis.SetLogger(_newRedisLogger(observer))

	dsn := fmt.Sprintf(_CACHE_REDIS_DSN, config.Host, config.Port)

	var ssl *tls.Config
	if config.SSLMode {
		ssl = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	poolConfig := &redis.Options{
		Addr:         dsn,
		TLSConfig:    ssl,
		Password:     config.Password,
		MinIdleConns: *config.MinConns,
		PoolSize:     *config.MaxConns,
		IdleTimeout:  *config.MaxConnIdleTime,
		MaxConnAge:   *config.MaxConnLifeTime,
		ReadTimeout:  *config.ReadTimeout,
		WriteTimeout: *config.WriteTimeout,
		DialTimeout:  *config.DialTimeout,
		PoolTimeout:  *config.DialTimeout,
	}

	var pool *redis.Client

	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		return util.ExponentialRetry(
			_retry.Attempts, _retry.InitialDelay, _retry.LimitDelay,
			_retry.Retriables, func(attempt int) error {
				var err error

				observer.Infof(ctx, "Trying to connect to the cache %d/%d", attempt, _retry.Attempts)

				pool = redis.NewClient(poolConfig)

				err = pool.Ping(ctx).Err()
				if err != nil {
					return ErrCacheGeneric.Raise().Cause(err)
				}

				return nil
			})
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return nil, ErrCacheTimedOut.Raise().Cause(err)
		}

		return nil, err
	}

	observer.Info(ctx, "Connected to the cache")

	cache := cache.New(&cache.Options{
		Redis:        pool,
		LocalCache:   nil,
		StatsEnabled: false,
	})

	return &Cache{
		observer: observer,
		config:   config,
		pool:     pool,
		cache:    cache,
	}, nil
}

func (self *Cache) Health(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		currentConns := self.pool.PoolStats().TotalConns
		if currentConns < uint32(*self.config.MinConns) {
			return ErrCacheUnhealthy.Raise().With("current conns %d below minimum %d",
				currentConns, *self.config.MinConns)
		}

		result, err := self.pool.Ping(ctx).Result()
		if err != nil || result != "PONG" {
			return ErrCacheUnhealthy.Raise().Cause(err)
		}

		err = ctx.Err()
		if err != nil {
			return ErrCacheUnhealthy.Raise().Cause(err)
		}

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrCacheTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}

func _chErrToError(err error) *errors.Error {
	if err == nil {
		return nil
	}

	switch err {
	case cache.ErrCacheMiss:
		return ErrCacheMiss.Raise().Skip(1).Cause(err)
	default:
		return ErrCacheGeneric.Raise().Skip(1).Cause(err)
	}
}

func (self *Cache) Set(ctx context.Context, key string, value any, ttl *time.Duration) error {
	if ttl == nil {
		ttl = util.Pointer(0 * time.Second)
	}

	err := self.cache.Set(&cache.Item{
		Ctx:            ctx,
		Key:            key,
		Value:          value,
		TTL:            *ttl,
		SkipLocalCache: false,
	})
	if err != nil {
		return _chErrToError(err)
	}

	return nil
}

func (self *Cache) Get(ctx context.Context, key string, dest any) error {
	err := self.cache.Get(ctx, key, dest)
	if err != nil {
		return _chErrToError(err)
	}

	return nil
}

func (self *Cache) Delete(ctx context.Context, key string) error {
	err := self.cache.Delete(ctx, key)
	if err != nil {
		return _chErrToError(err)
	}

	return nil
}

func (self *Cache) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info(ctx, "Closing cache")

		err := self.pool.Close()
		if err != nil {
			return ErrCacheGeneric.Raise().Cause(err)
		}

		self.observer.Info(ctx, "Closed cache")

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrCacheTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}

type _redisLogger struct {
	observer *Observer
}

func _newRedisLogger(observer *Observer) *_redisLogger {
	return &_redisLogger{
		observer: observer,
	}
}

func (self _redisLogger) Printf(ctx context.Context, format string, v ...any) {
	self.observer.Infof(ctx, format, v...)
}
