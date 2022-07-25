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
	"github.com/jackc/pgx/v4"
	gommon "github.com/labstack/gommon/log"
	"github.com/rs/zerolog"
	"github.com/scylladb/go-set/strset"
)

// type _key string

// const (
// 	// TODO: Move this to the correct site
// 	_DEFAULT_ASSETS_PATH    = "./assets"
// 	_DEFAULT_FILES_PATH     = _DEFAULT_ASSETS_PATH + "/files"
// 	_DEFAULT_IMAGES_PATH    = _DEFAULT_ASSETS_PATH + "/images"
// 	_DEFAULT_SCRIPTS_PATH   = _DEFAULT_ASSETS_PATH + "/scripts"
// 	_DEFAULT_STYLES_PATH    = _DEFAULT_ASSETS_PATH + "/styles"

// 	_BASE_KEY _key = "kit"
// 	// TODO: Move this to the correct site
// 	_DATABASE_TRANSACTION_KEY _key = ":database:transaction"
// )

type _environments struct {
	Development string
	Production  string
}

var Environments = _environments{
	Development: "dev",
	Production:  "prod",
}

type _errors struct {
	ErrDeadlineExceeded        func() *Error
	ErrLoggerGeneric           func() *Error
	ErrLoggerTimedOut          func() *Error
	ErrBinderGeneric           func() *Error
	ErrExceptionHandlerGeneric func() *Error
	ErrMigratorGeneric         func() *Error
	ErrMigratorTimedOut        func() *Error
	ErrObserverGeneric         func() *Error
	ErrObserverTimedOut        func() *Error
	ErrSerializerGeneric       func() *Error
	ErrRendererGeneric         func() *Error
}

// Errors contains the builtin errors.
var Errors = _errors{
	ErrDeadlineExceeded:        NewError("deadline exceeded"),
	ErrLoggerGeneric:           NewError("logger failed"),
	ErrLoggerTimedOut:          NewError("logger timed out"),
	ErrBinderGeneric:           NewError("binder failed"),
	ErrExceptionHandlerGeneric: NewError("error handler failed"),
	ErrMigratorGeneric:         NewError("migrator failed"),
	ErrMigratorTimedOut:        NewError("migrator timed out"),
	ErrObserverGeneric:         NewError("observer failed"),
	ErrObserverTimedOut:        NewError("observer timed out"),
	ErrSerializerGeneric:       NewError("serializer failed"),
	ErrRendererGeneric:         NewError("renderer failed"),
}

type _exceptions struct {
	// ExcServerGeneric generic server exception.
	ExcServerGeneric func() *Exception
	// ExcServerUnavailable server Unavailable exception.
	ExcServerUnavailable func() *Exception
	// ExcRequestTimeout request timeout exception.
	ExcRequestTimeout func() *Exception
	// ExcClientGeneric generic client exception.
	ExcClientGeneric func() *Exception
	// ExcInvalidRequest invalid request exception.
	ExcInvalidRequest func() *Exception
	// ExcNotFound not found exception.
	ExcNotFound func() *Exception
	// ExcUnauthorized unauthorized exception.
	ExcUnauthorized func() *Exception
}

// Exceptions contains the builtin exceptions.
var Exceptions = _exceptions{
	ExcServerGeneric:     NewException(http.StatusInternalServerError, "ERR_SERVER_GENERIC"),
	ExcServerUnavailable: NewException(http.StatusServiceUnavailable, "ERR_SERVER_UNAVAILABLE"),
	ExcRequestTimeout:    NewException(http.StatusRequestTimeout, "ERR_REQUEST_TIMEOUT"),
	ExcClientGeneric:     NewException(http.StatusBadRequest, "ERR_CLIENT_GENERIC"),
	ExcInvalidRequest:    NewException(http.StatusBadRequest, "ERR_INVALID_REQUEST"),
	ExcNotFound:          NewException(http.StatusNotFound, "ERR_NOT_FOUND"),
	ExcUnauthorized:      NewException(http.StatusUnauthorized, "ERR_UNAUTHORIZED"),
}

const (
	_UTILS_BYTE_BASE_SIZE        = 1024
	_UTILS_ASCII_LETTER_SET      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	_UTILS_ASCII_LETTER_SET_SIZE = 62
)

type _utils struct {
	ZlevelToGlevel map[zerolog.Level]gommon.Lvl
	GlevelToZlevel map[gommon.Lvl]zerolog.Level
	ZlevelToPlevel map[zerolog.Level]pgx.LogLevel
	PlevelToZlevel map[pgx.LogLevel]zerolog.Level
}

// Utils contains the builtin utils.
var Utils = _utils{
	ZlevelToGlevel: map[zerolog.Level]gommon.Lvl{
		zerolog.DebugLevel: gommon.DEBUG,
		zerolog.InfoLevel:  gommon.INFO,
		zerolog.WarnLevel:  gommon.WARN,
		zerolog.ErrorLevel: gommon.ERROR,
		zerolog.Disabled:   gommon.OFF,
	},
	GlevelToZlevel: map[gommon.Lvl]zerolog.Level{
		gommon.DEBUG: zerolog.DebugLevel,
		gommon.INFO:  zerolog.InfoLevel,
		gommon.WARN:  zerolog.WarnLevel,
		gommon.ERROR: zerolog.ErrorLevel,
		gommon.OFF:   zerolog.Disabled,
	},
	ZlevelToPlevel: map[zerolog.Level]pgx.LogLevel{
		zerolog.TraceLevel: pgx.LogLevelTrace,
		zerolog.DebugLevel: pgx.LogLevelDebug,
		zerolog.InfoLevel:  pgx.LogLevelInfo,
		zerolog.WarnLevel:  pgx.LogLevelWarn,
		zerolog.ErrorLevel: pgx.LogLevelError,
		zerolog.Disabled:   pgx.LogLevelNone,
	},
	PlevelToZlevel: map[pgx.LogLevel]zerolog.Level{
		pgx.LogLevelTrace: zerolog.TraceLevel,
		pgx.LogLevelDebug: zerolog.DebugLevel,
		pgx.LogLevelInfo:  zerolog.InfoLevel,
		pgx.LogLevelWarn:  zerolog.WarnLevel,
		pgx.LogLevelError: zerolog.ErrorLevel,
		pgx.LogLevelNone:  zerolog.Disabled,
	},
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

func (self _utils) EqualStringSlice(first *[]string, second *[]string) bool {
	if first == nil && second == nil {
		return true
	}

	if !(first != nil && second != nil) {
		return false
	}

	if len(*first) != len(*second) {
		return false
	}

	setFirst := strset.New((*first)...)
	setSecond := strset.New((*second)...)

	return strset.SymmetricDifference(setFirst, setSecond).IsEmpty()
}

func (self _utils) Deadline(ctx context.Context, work func(exceeded <-chan struct{}) error) error {
	if ctxDeadline, ok := ctx.Deadline(); ok {
		err := deadline.New(time.Until(ctxDeadline)).Run(work)
		if err == deadline.ErrTimedOut {
			err = Errors.ErrDeadlineExceeded()
		}

		return err
	}

	return work(nil)
}

func (self _utils) Retry(
	attempts int, delay time.Duration,
	classifier retrier.Classifier, work func(attempt int) error) error {
	// Go resiliency package does not count the first execution as an attempt
	attempts--
	if attempts < 0 {
		return nil
	}

	attempt := 1

	// nolint
	return retrier.New(retrier.ConstantBackoff(attempts, delay), classifier).
		Run(func() error {
			err := work(attempt)
			attempt++

			return err
		})
}

func (self _utils) ExponentialRetry(
	attempts int, initialDelay time.Duration, limitDelay time.Duration,
	classifier retrier.Classifier, work func(attempt int) error) error {
	// Go resiliency package does not count the first execution as an attempt
	attempts--
	if attempts < 0 {
		return nil
	}

	attempt := 1

	// nolint
	return retrier.New(retrier.LimitedExponentialBackoff(attempts, initialDelay, limitDelay), classifier).
		Run(func() error {
			err := work(attempt)
			attempt++

			return err
		})
}

func (self _utils) CopyInt(src *int) *int {
	if src == nil {
		return nil
	}

	dst := *src

	return &dst
}

func (self _utils) CopyIntSlice(src *[]int) *[]int {
	if src == nil {
		return nil
	}

	dst := make([]int, len(*src))
	copy(dst, *src)

	return &dst
}

func (self _utils) CopyIntMap(src *map[string]int) *map[string]int {
	if src == nil {
		return nil
	}

	dst := make(map[string]int, len(*src))
	for k, v := range *src {
		dst[k] = v
	}

	return &dst
}

func (self _utils) CopyIntSliceMap(src *map[string][]int) *map[string][]int {
	if src == nil {
		return nil
	}

	dst := make(map[string][]int, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyIntSlice(&v) // nolint
	}

	return &dst
}

func (self _utils) CopyString(src *string) *string {
	if src == nil {
		return nil
	}

	dst := *src

	return &dst
}

func (self _utils) CopyStringSlice(src *[]string) *[]string {
	if src == nil {
		return nil
	}

	dst := make([]string, len(*src))
	copy(dst, *src)

	return &dst
}

func (self _utils) CopyStringMap(src *map[string]string) *map[string]string {
	if src == nil {
		return nil
	}

	dst := make(map[string]string, len(*src))
	for k, v := range *src {
		dst[k] = v
	}

	return &dst
}

func (self _utils) CopyStringSliceMap(src *map[string][]string) *map[string][]string {
	if src == nil {
		return nil
	}

	dst := make(map[string][]string, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyStringSlice(&v) // nolint
	}

	return &dst
}

func (self _utils) CopyBool(src *bool) *bool {
	if src == nil {
		return nil
	}

	dst := *src

	return &dst
}

func (self _utils) CopyBoolSlice(src *[]bool) *[]bool {
	if src == nil {
		return nil
	}

	dst := make([]bool, len(*src))
	copy(dst, *src)

	return &dst
}

func (self _utils) CopyBoolMap(src *map[string]bool) *map[string]bool {
	if src == nil {
		return nil
	}

	dst := make(map[string]bool, len(*src))
	for k, v := range *src {
		dst[k] = v
	}

	return &dst
}

func (self _utils) CopyBoolSliceMap(src *map[string][]bool) *map[string][]bool {
	if src == nil {
		return nil
	}

	dst := make(map[string][]bool, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyBoolSlice(&v) // nolint
	}

	return &dst
}

func (self _utils) CopyTime(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}

	dstYear, dstMonth, dstDay := src.Date()
	dstHour, dstMin, dstSec := src.Clock()
	dstNsec := src.Nanosecond()
	dstLocation := *src.Location()

	dst := time.Date(dstYear, dstMonth, dstDay, dstHour, dstMin, dstSec, dstNsec, &dstLocation)

	return &dst
}

func (self _utils) CopyTimeSlice(src *[]time.Time) *[]time.Time {
	if src == nil {
		return nil
	}

	dst := make([]time.Time, len(*src))
	for i := 0; i < len(dst); i++ {
		dst[i] = *Utils.CopyTime(&(*src)[i])
	}

	return &dst
}

func (self _utils) CopyTimeMap(src *map[string]time.Time) *map[string]time.Time {
	if src == nil {
		return nil
	}

	dst := make(map[string]time.Time, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyTime(&v) // nolint
	}

	return &dst
}

func (self _utils) CopyTimeSliceMap(src *map[string][]time.Time) *map[string][]time.Time {
	if src == nil {
		return nil
	}

	dst := make(map[string][]time.Time, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyTimeSlice(&v) // nolint
	}

	return &dst
}

func (self _utils) CopyDate(src *date.Date) *date.Date {
	if src == nil {
		return nil
	}

	dst := date.FromTime(src.Time)

	return &dst
}

func (self _utils) CopyDateSlice(src *[]date.Date) *[]date.Date {
	if src == nil {
		return nil
	}

	dst := make([]date.Date, len(*src))
	for i := 0; i < len(dst); i++ {
		dst[i] = *Utils.CopyDate(&(*src)[i])
	}

	return &dst
}

func (self _utils) CopyDateMap(src *map[string]date.Date) *map[string]date.Date {
	if src == nil {
		return nil
	}

	dst := make(map[string]date.Date, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyDate(&v) // nolint
	}

	return &dst
}

func (self _utils) CopyDateSliceMap(src *map[string][]date.Date) *map[string][]date.Date {
	if src == nil {
		return nil
	}

	dst := make(map[string][]date.Date, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyDateSlice(&v) // nolint
	}

	return &dst
}
