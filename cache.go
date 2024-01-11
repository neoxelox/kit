package kit

import (
	"context"
	"crypto/tls"
	"fmt"
	"runtime"
	"time"

	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/v8"
	"github.com/neoxelox/kit/util"
)

const (
	_CACHE_REDIS_DSN = "%s:%d"
)

var (
	_CACHE_DEFAULT_CONFIG = CacheConfig{
		CacheMinConns:        util.Pointer(1),
		CacheMaxConns:        util.Pointer(10 * runtime.GOMAXPROCS(-1)),
		CacheMaxConnIdleTime: util.Pointer(30 * time.Minute),
		CacheMaxConnLifeTime: util.Pointer(1 * time.Hour),
		CacheReadTimeout:     util.Pointer(30 * time.Second),
		CacheWriteTimeout:    util.Pointer(30 * time.Second),
		CacheDialTimeout:     util.Pointer(30 * time.Second),
		CacheLocalConfig:     nil,
	}

	_CACHE_DEFAULT_RETRY_CONFIG = RetryConfig{
		Attempts:     1,
		InitialDelay: 0 * time.Second,
		LimitDelay:   0 * time.Second,
		Retriables:   []error{},
	}
)

type CacheLocalConfig struct {
	Size int
	TTL  time.Duration
}

type CacheConfig struct {
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
	CacheLocalConfig     *CacheLocalConfig
}

type Cache struct {
	config   CacheConfig
	observer Observer
	pool     *redis.Client
	cache    *cache.Cache
}

func NewCache(ctx context.Context, observer Observer, config CacheConfig, retry ...RetryConfig) (*Cache, error) {
	util.Merge(&config, _CACHE_DEFAULT_CONFIG)
	_retry := util.Optional(retry, _CACHE_DEFAULT_RETRY_CONFIG)

	redis.SetLogger(_newRedisLogger(&observer))

	dsn := fmt.Sprintf(_CACHE_REDIS_DSN, config.CacheHost, config.CachePort)

	var ssl *tls.Config
	if config.CacheSSLMode {
		ssl = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	poolConfig := &redis.Options{
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

	var localCache cache.LocalCache
	if config.CacheLocalConfig != nil {
		localCache = cache.NewTinyLFU(config.CacheLocalConfig.Size, config.CacheLocalConfig.TTL)
	}

	var pool *redis.Client

	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		return util.ExponentialRetry(
			_retry.Attempts, _retry.InitialDelay, _retry.LimitDelay,
			_retry.Retriables, func(attempt int) error {
				var err error // nolint

				observer.Infof(ctx, "Trying to connect to the cache %d/%d", attempt, _retry.Attempts)

				pool = redis.NewClient(poolConfig)

				err = pool.Ping(ctx).Err()
				if err != nil {
					return ErrCacheGeneric().WrapAs(err)
				}

				return nil
			})
	})
	switch {
	case err == nil:
	case util.ErrDeadlineExceeded.Is(err):
		return nil, ErrCacheTimedOut()
	default:
		return nil, ErrCacheGeneric().Wrap(err)
	}

	observer.Info(ctx, "Connected to the cache")

	cache := cache.New(&cache.Options{
		Redis:        pool,
		LocalCache:   localCache,
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
		if currentConns < uint32(*self.config.CacheMinConns) {
			return ErrCacheUnhealthy().Withf("current conns %d below minimum %d",
				currentConns, *self.config.CacheMinConns)
		}

		result, err := self.pool.Ping(ctx).Result()
		if err != nil || result != "PONG" {
			return ErrCacheUnhealthy().WrapAs(err)
		}

		err = ctx.Err()
		if err != nil {
			return ErrCacheUnhealthy().WrapAs(err)
		}

		return nil
	})
	switch {
	case err == nil:
		return nil
	case util.ErrDeadlineExceeded.Is(err):
		return ErrCacheTimedOut()
	default:
		return ErrCacheGeneric().Wrap(err)
	}
}

func _chErrToError(err error) *Error {
	if err == nil {
		return nil
	}

	switch err {
	case cache.ErrCacheMiss:
		return ErrCacheMiss().WrapWithDepth(1, err)
	default:
		return ErrCacheGeneric().WrapWithDepth(1, err)
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
			return ErrCacheGeneric().WrapAs(err)
		}

		self.observer.Info(ctx, "Closed cache")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case util.ErrDeadlineExceeded.Is(err):
		return ErrCacheTimedOut()
	default:
		return ErrCacheGeneric().Wrap(err)
	}
}

type _redisLogger struct {
	observer *Observer
}

func _newRedisLogger(observer *Observer) *_redisLogger {
	return &_redisLogger{
		observer: observer,
	}
}

func (self _redisLogger) Printf(ctx context.Context, format string, v ...any) { // nolint
	self.observer.Infof(ctx, format, v...)
}
