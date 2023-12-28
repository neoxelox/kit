package kit

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/v8"
)

const (
	_CACHE_REDIS_DSN = "%s:%d"
)

var (
	_CACHE_DEFAULT_MIN_CONNS           = 1
	_CACHE_DEFAULT_MAX_CONNS           = 10 * runtime.GOMAXPROCS(-1)
	_CACHE_DEFAULT_MAX_CONN_IDLE_TIME  = 30 * time.Minute
	_CACHE_DEFAULT_MAX_CONN_LIFE_TIME  = 1 * time.Hour
	_CACHE_DEFAULT_READ_TIMEOUT        = 30 * time.Second
	_CACHE_DEFAULT_WRITE_TIMEOUT       = 30 * time.Second
	_CACHE_DEFAULT_DIAL_TIMEOUT        = 30 * time.Second
	_CACHE_DEFAULT_ACQUIRE_TIMEOUT     = 30 * time.Second
	_CACHE_DEFAULT_RETRY_ATTEMPTS      = 1
	_CACHE_DEFAULT_RETRY_INITIAL_DELAY = 0 * time.Second
	_CACHE_DEFAULT_RETRY_LIMIT_DELAY   = 0 * time.Second
)

type CacheRetryConfig struct {
	Attempts     int
	InitialDelay time.Duration
	LimitDelay   time.Duration
}

type CacheLocalConfig struct {
	Size int
	TTL  time.Duration
}

type CacheConfig struct {
	CacheHost            string
	CachePort            int
	CachePassword        string
	CacheMinConns        *int
	CacheMaxConns        *int
	CacheMaxConnIdleTime *time.Duration
	CacheMaxConnLifeTime *time.Duration
	CacheReadTimeout     *time.Duration
	CacheWriteTimeout    *time.Duration
	CacheDialTimeout     *time.Duration
	CacheAcquireTimeout  *time.Duration
	CacheLocalConfig     *CacheLocalConfig
}

type Cache struct {
	config   CacheConfig
	observer Observer
	pool     *redis.Client
	cache    *cache.Cache
}

func NewCache(ctx context.Context, observer Observer, config CacheConfig, retry *CacheRetryConfig) (*Cache, error) {
	if config.CacheMinConns == nil {
		config.CacheMinConns = ptr(_CACHE_DEFAULT_MIN_CONNS)
	}

	if config.CacheMaxConns == nil {
		config.CacheMaxConns = ptr(_CACHE_DEFAULT_MAX_CONNS)
	}

	if config.CacheMaxConnIdleTime == nil {
		config.CacheMaxConnIdleTime = ptr(_CACHE_DEFAULT_MAX_CONN_IDLE_TIME)
	}

	if config.CacheMaxConnLifeTime == nil {
		config.CacheMaxConnLifeTime = ptr(_CACHE_DEFAULT_MAX_CONN_LIFE_TIME)
	}

	if config.CacheReadTimeout == nil {
		config.CacheReadTimeout = ptr(_CACHE_DEFAULT_READ_TIMEOUT)
	}

	if config.CacheWriteTimeout == nil {
		config.CacheWriteTimeout = ptr(_CACHE_DEFAULT_WRITE_TIMEOUT)
	}

	if config.CacheDialTimeout == nil {
		config.CacheDialTimeout = ptr(_CACHE_DEFAULT_DIAL_TIMEOUT)
	}

	if config.CacheAcquireTimeout == nil {
		config.CacheAcquireTimeout = ptr(_CACHE_DEFAULT_ACQUIRE_TIMEOUT)
	}

	if retry == nil {
		retry = &CacheRetryConfig{
			Attempts:     _CACHE_DEFAULT_RETRY_ATTEMPTS,
			InitialDelay: _CACHE_DEFAULT_RETRY_INITIAL_DELAY,
			LimitDelay:   _CACHE_DEFAULT_RETRY_LIMIT_DELAY,
		}
	}

	redis.SetLogger(_newRedisLogger(&observer))

	dsn := fmt.Sprintf(_CACHE_REDIS_DSN, config.CacheHost, config.CachePort)

	poolConfig := &redis.Options{
		Addr:         dsn,
		Password:     config.CachePassword,
		MinIdleConns: *config.CacheMinConns,
		PoolSize:     *config.CacheMaxConns,
		IdleTimeout:  *config.CacheMaxConnIdleTime,
		MaxConnAge:   *config.CacheMaxConnLifeTime,
		ReadTimeout:  *config.CacheReadTimeout,
		WriteTimeout: *config.CacheWriteTimeout,
		DialTimeout:  *config.CacheDialTimeout,
		PoolTimeout:  *config.CacheAcquireTimeout,
	}

	var localCache *cache.TinyLFU
	if config.CacheLocalConfig != nil {
		localCache = cache.NewTinyLFU(config.CacheLocalConfig.Size, config.CacheLocalConfig.TTL)
	}

	var pool *redis.Client

	// TODO: only retry on specific errors
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		return Utils.ExponentialRetry(
			retry.Attempts, retry.InitialDelay, retry.LimitDelay,
			nil, func(attempt int) error {
				var err error // nolint

				observer.Infof(ctx, "Trying to connect to the cache %d/%d", attempt, retry.Attempts)

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
	case ErrDeadlineExceeded().Is(err):
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
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
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
	case ErrDeadlineExceeded().Is(err):
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
		ttl = ptr(0 * time.Second)
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
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
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
	case ErrDeadlineExceeded().Is(err):
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
