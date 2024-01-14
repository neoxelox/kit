// Package kit implements a highly opinionated Go backend kit.
package kit

import (
	"time"
)

type Environment string

var (
	EnvDevelopment Environment = "dev"
	EnvIntegration Environment = "ci"
	EnvProduction  Environment = "prod"
)

type Key string

var KeyBase Key = "kit:"

type RetryConfig struct {
	Attempts     int
	InitialDelay time.Duration
	LimitDelay   time.Duration
	Retriables   []error
}
