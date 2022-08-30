// Package kit implements a highly opitionated Go backend kit.
package kit

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aodin/date"
	"github.com/cockroachdb/errors"
	"github.com/eapache/go-resiliency/deadline"
	"github.com/eapache/go-resiliency/retrier"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cpy/cpy"
)

func ptr[T any](v T) *T {
	return &v
}

type _key string

// Builtin context/cache keys.
var (
	KeyBase                _key = "kit:"
	KeyDatabaseTransaction _key = KeyBase + "database:transaction"
	KeyLocalizerLocale     _key = KeyBase + "localizer:locale"
	KeyTraceID             _key = KeyBase + "trace:id"
)

type _environment string

// Builtin environments.
var (
	EnvDevelopment _environment = "dev"
	EnvProduction  _environment = "prod"
)

type _level int

// Builtin levels.
var (
	LvlTrace _level = -5
	LvlDebug _level = -4
	LvlInfo  _level = -3
	LvlWarn  _level = -2
	LvlError _level = -1
	LvlNone  _level
)

// Builtin errors.
var (
	ErrDeadlineExceeded           = NewError("deadline exceeded")
	ErrLoggerGeneric              = NewError("logger failed")
	ErrLoggerTimedOut             = NewError("logger timed out")
	ErrBinderGeneric              = NewError("binder failed")
	ErrExceptionHandlerGeneric    = NewError("error handler failed")
	ErrMigratorGeneric            = NewError("migrator failed")
	ErrMigratorTimedOut           = NewError("migrator timed out")
	ErrObserverGeneric            = NewError("observer failed")
	ErrObserverTimedOut           = NewError("observer timed out")
	ErrSerializerGeneric          = NewError("serializer failed")
	ErrRendererGeneric            = NewError("renderer failed")
	ErrLocalizerGeneric           = NewError("localizer failed")
	ErrServerGeneric              = NewError("server failed")
	ErrServerTimedOut             = NewError("server timed out")
	ErrDatabaseGeneric            = NewError("database failed")
	ErrDatabaseTimedOut           = NewError("database timed out")
	ErrDatabaseUnhealthy          = NewError("database unhealthy")
	ErrDatabaseTransactionFailed  = NewError("database transaction failed")
	ErrDatabaseNoRows             = NewError("database no rows in result set")
	ErrDatabaseIntegrityViolation = NewError("database integrity constraint violation")
	ErrCacheGeneric               = NewError("cache failed")
	ErrCacheTimedOut              = NewError("cache timed out")
	ErrCacheUnhealthy             = NewError("cache unhealthy")
	ErrCacheMiss                  = NewError("cache key not found")
)

// Builtin exceptions.
var (
	// ExcServerGeneric generic server exception.
	ExcServerGeneric = NewException(http.StatusInternalServerError, "ERR_SERVER_GENERIC")
	// ExcServerUnavailable server Unavailable exception.
	ExcServerUnavailable = NewException(http.StatusServiceUnavailable, "ERR_SERVER_UNAVAILABLE")
	// ExcRequestTimeout request timeout exception.
	ExcRequestTimeout = NewException(http.StatusGatewayTimeout, "ERR_REQUEST_TIMEOUT")
	// ExcClientGeneric generic client exception.
	ExcClientGeneric = NewException(http.StatusBadRequest, "ERR_CLIENT_GENERIC")
	// ExcInvalidRequest invalid request exception.
	ExcInvalidRequest = NewException(http.StatusBadRequest, "ERR_INVALID_REQUEST")
	// ExcNotFound not found exception.
	ExcNotFound = NewException(http.StatusNotFound, "ERR_NOT_FOUND")
	// ExcUnauthorized unauthorized exception.
	ExcUnauthorized = NewException(http.StatusUnauthorized, "ERR_UNAUTHORIZED")
)

const (
	_UTILS_BYTE_BASE_SIZE        = 1024
	_UTILS_ASCII_LETTER_SET      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	_UTILS_ASCII_LETTER_SET_SIZE = 62
)

type _utils struct {
	copier *cpy.Copier
}

// Utils contains the builtin utils.
var Utils = _utils{
	copier: cpy.New(cpy.IgnoreAllUnexported(), cpy.Shallow(time.Time{}), cpy.Shallow(date.Date{})),
}

func (self _utils) ByteSize(size int) string {
	if size < _UTILS_BYTE_BASE_SIZE {
		return fmt.Sprintf("%dB", size)
	}

	div, exp := int64(_UTILS_BYTE_BASE_SIZE), 0
	for n := size / _UTILS_BYTE_BASE_SIZE; n >= _UTILS_BYTE_BASE_SIZE; n /= _UTILS_BYTE_BASE_SIZE {
		div *= _UTILS_BYTE_BASE_SIZE
		exp++
	}

	number := fmt.Sprintf("%.1f", float64(size)/float64(div))
	exponent := "KMGTPE"[exp]

	return fmt.Sprintf("%s%cB", strings.TrimRight(strings.TrimRight(number, "0"), "."), exponent)
}

func (self _utils) RandomString(length int) string {
	bts := make([]byte, length)

	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(_UTILS_ASCII_LETTER_SET_SIZE)))
		if err != nil {
			panic(err)
		}

		bts[i] = _UTILS_ASCII_LETTER_SET[num.Int64()]
	}

	return string(bts)
}

func (self _utils) CombineErrors(first error, second error) error {
	return errors.CombineErrors(first, second) // nolint
}

func (self _utils) GetEnvAsString(key string, def string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return def
}

func (self _utils) GetEnvAsInt(key string, def int) int {
	valueStr := Utils.GetEnvAsString(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}

	return def
}

func (self _utils) GetEnvAsBool(key string, def bool) bool {
	valueStr := Utils.GetEnvAsString(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}

	return def
}

func (self _utils) GetEnvAsSlice(key string, def []string) []string {
	valueStr := Utils.GetEnvAsString(key, "")
	if value := strings.Split(valueStr, ","); len(value) >= 1 {
		return value
	}

	return def
}

func (self _utils) Deadline(ctx context.Context, fn func(exceeded <-chan struct{}) error) error {
	if ctxDeadline, ok := ctx.Deadline(); ok {
		err := deadline.New(time.Until(ctxDeadline)).Run(fn)
		if err == deadline.ErrTimedOut {
			err = ErrDeadlineExceeded()
		}

		return err
	}

	return fn(nil)
}

func (self _utils) Retry(
	attempts int, delay time.Duration,
	classifier retrier.Classifier, fn func(attempt int) error) error {
	// Go resiliency package does not count the first execution as an attempt
	attempts--
	if attempts < 0 {
		return nil
	}

	attempt := 1

	// nolint
	return retrier.New(retrier.ConstantBackoff(attempts, delay), classifier).
		Run(func() error {
			err := fn(attempt)
			attempt++

			return err
		})
}

func (self _utils) ExponentialRetry(
	attempts int, initialDelay time.Duration, limitDelay time.Duration,
	classifier retrier.Classifier, fn func(attempt int) error) error {
	// Go resiliency package does not count the first execution as an attempt
	attempts--
	if attempts < 0 {
		return nil
	}

	attempt := 1

	// nolint
	return retrier.New(retrier.LimitedExponentialBackoff(attempts, initialDelay, limitDelay), classifier).
		Run(func() error {
			err := fn(attempt)
			attempt++

			return err
		})
}

func (self _utils) Copy(src interface{}) interface{} {
	return self.copier.Copy(src)
}

func (self _utils) Equals(first interface{}, second interface{}) bool {
	return cmp.Equal(first, second)
}
