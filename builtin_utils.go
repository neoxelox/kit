package kit

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aodin/date"
	"github.com/cockroachdb/errors"
	"github.com/scylladb/go-set/strset"
)

type utils struct{}

var Utils utils

const (
	_BASE_SIZE             = 1024
	_ASCII_LETTER_SET      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	_ASCII_LETTER_SET_SIZE = 62
)

func (self utils) ByteSize(size int) string {
	if size < _BASE_SIZE {
		return fmt.Sprintf("%dB", size)
	}

	div, exp := int64(_BASE_SIZE), 0
	for n := size / _BASE_SIZE; n >= _BASE_SIZE; n /= _BASE_SIZE {
		div *= _BASE_SIZE
		exp++
	}

	number := fmt.Sprintf("%.1f", float64(size)/float64(div))
	exponent := "KMGTPE"[exp]

	return fmt.Sprintf("%s%cB", strings.TrimRight(strings.TrimRight(number, "0"), "."), exponent)
}

func (self utils) RandomString(length int) string {
	bts := make([]byte, length)

	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(_ASCII_LETTER_SET_SIZE)))
		if err != nil {
			panic(err)
		}

		bts[i] = _ASCII_LETTER_SET[num.Int64()]
	}

	return string(bts)
}

func (self utils) CombineErrors(first error, second error) error {
	return errors.CombineErrors(first, second) // nolint
}

func (self utils) GetEnvAsString(key string, def string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return def
}

func (self utils) GetEnvAsInt(key string, def int) int {
	valueStr := Utils.GetEnvAsString(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}

	return def
}

func (self utils) GetEnvAsBool(key string, def bool) bool {
	valueStr := Utils.GetEnvAsString(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}

	return def
}

func (self utils) GetEnvAsSlice(key string, def []string) []string {
	valueStr := Utils.GetEnvAsString(key, "")
	if value := strings.Split(valueStr, ","); len(value) >= 1 {
		return value
	}

	return def
}

func (self utils) EqualStringSlice(first *[]string, second *[]string) bool {
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

func (self utils) CopyInt(src *int) *int {
	if src == nil {
		return nil
	}

	dst := *src

	return &dst
}

func (self utils) CopyIntSlice(src *[]int) *[]int {
	if src == nil {
		return nil
	}

	dst := make([]int, len(*src))
	copy(dst, *src)

	return &dst
}

func (self utils) CopyIntMap(src *map[string]int) *map[string]int {
	if src == nil {
		return nil
	}

	dst := make(map[string]int, len(*src))
	for k, v := range *src {
		dst[k] = v
	}

	return &dst
}

func (self utils) CopyIntSliceMap(src *map[string][]int) *map[string][]int {
	if src == nil {
		return nil
	}

	dst := make(map[string][]int, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyIntSlice(&v) // nolint
	}

	return &dst
}

func (self utils) CopyString(src *string) *string {
	if src == nil {
		return nil
	}

	dst := *src

	return &dst
}

func (self utils) CopyStringSlice(src *[]string) *[]string {
	if src == nil {
		return nil
	}

	dst := make([]string, len(*src))
	copy(dst, *src)

	return &dst
}

func (self utils) CopyStringMap(src *map[string]string) *map[string]string {
	if src == nil {
		return nil
	}

	dst := make(map[string]string, len(*src))
	for k, v := range *src {
		dst[k] = v
	}

	return &dst
}

func (self utils) CopyStringSliceMap(src *map[string][]string) *map[string][]string {
	if src == nil {
		return nil
	}

	dst := make(map[string][]string, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyStringSlice(&v) // nolint
	}

	return &dst
}

func (self utils) CopyBool(src *bool) *bool {
	if src == nil {
		return nil
	}

	dst := *src

	return &dst
}

func (self utils) CopyBoolSlice(src *[]bool) *[]bool {
	if src == nil {
		return nil
	}

	dst := make([]bool, len(*src))
	copy(dst, *src)

	return &dst
}

func (self utils) CopyBoolMap(src *map[string]bool) *map[string]bool {
	if src == nil {
		return nil
	}

	dst := make(map[string]bool, len(*src))
	for k, v := range *src {
		dst[k] = v
	}

	return &dst
}

func (self utils) CopyBoolSliceMap(src *map[string][]bool) *map[string][]bool {
	if src == nil {
		return nil
	}

	dst := make(map[string][]bool, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyBoolSlice(&v) // nolint
	}

	return &dst
}

func (self utils) CopyTime(src *time.Time) *time.Time {
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

func (self utils) CopyTimeSlice(src *[]time.Time) *[]time.Time {
	if src == nil {
		return nil
	}

	dst := make([]time.Time, len(*src))
	for i := 0; i < len(dst); i++ {
		dst[i] = *Utils.CopyTime(&(*src)[i])
	}

	return &dst
}

func (self utils) CopyTimeMap(src *map[string]time.Time) *map[string]time.Time {
	if src == nil {
		return nil
	}

	dst := make(map[string]time.Time, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyTime(&v) // nolint
	}

	return &dst
}

func (self utils) CopyTimeSliceMap(src *map[string][]time.Time) *map[string][]time.Time {
	if src == nil {
		return nil
	}

	dst := make(map[string][]time.Time, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyTimeSlice(&v) // nolint
	}

	return &dst
}

func (self utils) CopyDate(src *date.Date) *date.Date {
	if src == nil {
		return nil
	}

	dst := date.FromTime(src.Time)

	return &dst
}

func (self utils) CopyDateSlice(src *[]date.Date) *[]date.Date {
	if src == nil {
		return nil
	}

	dst := make([]date.Date, len(*src))
	for i := 0; i < len(dst); i++ {
		dst[i] = *Utils.CopyDate(&(*src)[i])
	}

	return &dst
}

func (self utils) CopyDateMap(src *map[string]date.Date) *map[string]date.Date {
	if src == nil {
		return nil
	}

	dst := make(map[string]date.Date, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyDate(&v) // nolint
	}

	return &dst
}

func (self utils) CopyDateSliceMap(src *map[string][]date.Date) *map[string][]date.Date {
	if src == nil {
		return nil
	}

	dst := make(map[string][]date.Date, len(*src))
	for k, v := range *src {
		dst[k] = *Utils.CopyDateSlice(&v) // nolint
	}

	return &dst
}
