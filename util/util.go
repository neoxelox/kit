package util

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"dario.cat/mergo"
	"github.com/aodin/date"
	"github.com/eapache/go-resiliency/deadline"
	"github.com/eapache/go-resiliency/retrier"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cpy/cpy"
	"github.com/neoxelox/errors"
)

const (
	_UTIL_BYTE_BASE_SIZE        = 1024
	_UTIL_ASCII_LETTER_SET      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	_UTIL_ASCII_LETTER_SET_SIZE = 62
	_UTIL_ENV_SLICE_SEPARATOR   = ","
)

var ErrDeadlineExceeded = errors.New("deadline exceeded")

var copier = cpy.New(cpy.IgnoreAllUnexported(), cpy.Shallow(time.Time{}), cpy.Shallow(date.Date{}))

func ByteSize(size int) string {
	if size < _UTIL_BYTE_BASE_SIZE {
		return fmt.Sprintf("%dB", size)
	}

	div := int64(_UTIL_BYTE_BASE_SIZE)
	exp := 0

	for n := size / _UTIL_BYTE_BASE_SIZE; n >= _UTIL_BYTE_BASE_SIZE; n /= _UTIL_BYTE_BASE_SIZE {
		div *= _UTIL_BYTE_BASE_SIZE
		exp++
	}

	number := fmt.Sprintf("%.1f", float64(size)/float64(div))
	exponent := "KMGTPE"[exp]

	return fmt.Sprintf("%s%cB", strings.TrimRight(strings.TrimRight(number, "0"), "."), exponent)
}

func RandomString(length int) string {
	bytes := make([]byte, length)

	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(_UTIL_ASCII_LETTER_SET_SIZE)))
		if err != nil {
			panic(err)
		}

		bytes[i] = _UTIL_ASCII_LETTER_SET[num.Int64()]
	}

	return string(bytes)
}

func GetEnv[T string | int | bool | []string | []int | []bool](key string, def T) T {
	value, exists := os.LookupEnv(key)
	if !exists {
		return def
	}

	ret := def
	switch pret := any(&ret).(type) {
	case *string:
		*pret = value

	case *int:
		value, _ := strconv.ParseInt(value, 10, 0)
		*pret = int(value)

	case *bool:
		value, _ := strconv.ParseBool(value)
		*pret = value

	case *[]string:
		value := strings.Split(value, _UTIL_ENV_SLICE_SEPARATOR)
		*pret = value

	case *[]int:
		values := strings.Split(value, _UTIL_ENV_SLICE_SEPARATOR)
		value := make([]int, 0, len(values))
		for _, val := range values {
			val, _ := strconv.ParseInt(val, 10, 0)
			value = append(value, int(val))
		}
		*pret = value

	case *[]bool:
		values := strings.Split(value, _UTIL_ENV_SLICE_SEPARATOR)
		value := make([]bool, 0, len(values))
		for _, val := range values {
			val, _ := strconv.ParseBool(val)
			value = append(value, val)
		}
		*pret = value
	}

	return ret
}

func Pointer[T any](variable T) *T {
	return &variable
}

func Optional[T any](param []T, def T) T {
	if len(param) > 0 {
		return param[0]
	}

	return def
}

func Deadline(ctx context.Context, fn func(exceeded <-chan struct{}) error) error {
	if ctxDeadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(ctxDeadline)

		err := deadline.New(timeout).Run(fn)
		if err == deadline.ErrTimedOut {
			err = ErrDeadlineExceeded.Raise().
				Extra(map[string]any{"timeout": timeout}).Cause(err)
		}

		return err
	}

	return fn(nil)
}

func Retry(attempts int, delay time.Duration, retriables []error, fn func(attempt int) error) error {
	// Go resiliency package does not count the first execution as an attempt
	attempts--
	if attempts < 0 {
		return nil
	}

	var classifier retrier.Classifier
	if len(retriables) > 0 {
		classifier = retrier.WhitelistClassifier(retriables)
	}

	attempt := 1

	return retrier.New(retrier.ConstantBackoff(attempts, delay), classifier).
		Run(func() error {
			err := fn(attempt)
			attempt++

			return err
		})
}

func ExponentialRetry(attempts int, initialDelay time.Duration, limitDelay time.Duration,
	retriables []error, fn func(attempt int) error) error {
	// Go resiliency package does not count the first execution as an attempt
	attempts--
	if attempts < 0 {
		return nil
	}

	var classifier retrier.Classifier
	if len(retriables) > 0 {
		classifier = retrier.WhitelistClassifier(retriables)
	}

	attempt := 1

	return retrier.New(retrier.LimitedExponentialBackoff(attempts, initialDelay, limitDelay), classifier).
		Run(func() error {
			err := fn(attempt)
			attempt++

			return err
		})
}

func Equals(first any, second any) bool {
	return cmp.Equal(first, second)
}

func Copy[T any](src T) *T {
	return copier.Copy(&src).(*T)
}

func Merge[T any](dst *T, src T) {
	err := mergo.Merge(dst, src)
	if err != nil {
		panic(err)
	}
}
