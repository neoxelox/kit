package kit

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

var (
	_OBSERVER_DEFAULT_RETRY_ATTEMPTS       = 1
	_OBSERVER_DEFAULT_RETRY_INITIAL_DELAY  = 0 * time.Second
	_OBSERVER_DEFAULT_RETRY_LIMIT_DELAY    = 0 * time.Second
	_OBSERVER_DEFAULT_SENTRY_FLUSH_TIMEOUT = 2 * time.Second
)

type ObserverRetryConfig struct {
	Attempts     int
	InitialDelay time.Duration
	LimitDelay   time.Duration
}

type ObserverSentryConfig struct {
	Dsn string
}

type ObserverConfig struct {
	Environment  string
	Release      string
	AppName      string
	Level        *zerolog.Level // TODO: use agnostic level
	SentryConfig *ObserverSentryConfig
	RetryConfig  *ObserverRetryConfig
}

type Observer struct {
	config ObserverConfig
	Logger
}

func NewObserver(ctx context.Context, config ObserverConfig) (*Observer, error) {
	if config.RetryConfig == nil {
		config.RetryConfig = &ObserverRetryConfig{
			Attempts:     _OBSERVER_DEFAULT_RETRY_ATTEMPTS,
			InitialDelay: _OBSERVER_DEFAULT_RETRY_INITIAL_DELAY,
			LimitDelay:   _OBSERVER_DEFAULT_RETRY_LIMIT_DELAY,
		}
	}

	logger := NewLogger(LoggerConfig{
		Environment: config.Environment,
		AppName:     config.AppName,
		Level:       config.Level,
	})

	if config.SentryConfig != nil {
		// TODO: only retry on specific errors
		err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
			return Utils.ExponentialRetry(
				config.RetryConfig.Attempts, config.RetryConfig.InitialDelay, config.RetryConfig.LimitDelay,
				nil, func(attempt int) error {
					logger.Infof("Trying to connect to the Sentry service %d/%d", attempt, config.RetryConfig.Attempts)

					err := sentry.Init(sentry.ClientOptions{
						Dsn:              config.SentryConfig.Dsn,
						Environment:      config.Environment,
						Release:          config.Release,
						ServerName:       config.AppName,
						Debug:            false,
						AttachStacktrace: false, // Already done by errors package
						SampleRate:       1.0,   // Error events
						TracesSampleRate: 0,     // Transaction events. TODO: activate?
					})
					if err != nil {
						return Errors.ErrObserverGeneric().WrapAs(err)
					}

					return nil
				})
		})
		switch {
		case err == nil:
		case Errors.ErrDeadlineExceeded().Is(err):
			return nil, Errors.ErrObserverTimedOut()
		default:
			return nil, Errors.ErrObserverGeneric().Wrap(err)
		}

		logger.Info("Connected to the Sentry service")
	}

	return &Observer{
		config: config,
		Logger: *logger,
	}, nil
}

func (self *Observer) Anchor() {
	self.Logger.SetFile(2) // nolint
}

func (self Observer) Error(i ...interface{}) {
	self.Logger.Error(i...)

	if self.config.SentryConfig != nil {
		for _, ie := range i {
			var sentryEvent *sentry.Event
			var sentryEventExtra map[string]interface{}

			switch err := ie.(type) {
			case nil:
				continue
			case *Error:
				sentryEvent, sentryEventExtra = errors.BuildSentryReport(err.Unwrap())
			case *Exception:
				sentryEvent, sentryEventExtra = errors.BuildSentryReport(err.Unwrap())
			case error:
				sentryEvent, sentryEventExtra = errors.BuildSentryReport(err)
			default:
				sentryEvent, sentryEventExtra = errors.BuildSentryReport(errors.Errorf("%+v", err))
			}

			for k, v := range sentryEventExtra {
				sentryEvent.Extra[k] = v
			}

			sentryEvent.Level = sentry.LevelError

			// TODO: enhance exception message and title

			sentry.CaptureEvent(sentryEvent)
		}
	}
}

func (self Observer) Errorf(format string, i ...interface{}) {
	self.Error(fmt.Sprintf(format, i...))
}

// TODO
func (self Observer) Metric() {

}

// TODO
func (self Observer) Trace() func() {
	return func() {}
}

func (self Observer) Flush(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		err := self.Logger.Flush(ctx)
		if err != nil {
			return Errors.ErrObserverGeneric().WrapAs(err)
		}

		if self.config.SentryConfig != nil {
			sentryFlushTimeout := _OBSERVER_DEFAULT_SENTRY_FLUSH_TIMEOUT
			if ctxDeadline, ok := ctx.Deadline(); ok {
				sentryFlushTimeout = time.Until(ctxDeadline)
			}

			ok := sentry.Flush(sentryFlushTimeout)
			if !ok {
				return Errors.ErrObserverGeneric().With("sentry lost events while flushing")
			}
		}

		return nil
	})
	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrObserverTimedOut()
	default:
		return Errors.ErrObserverGeneric().Wrap(err)
	}
}

func (self Observer) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.Logger.Info("Closing observer")

		err := self.Flush(ctx)
		if err != nil {
			return Errors.ErrObserverGeneric().WrapAs(err)
		}

		if self.config.SentryConfig != nil {
			// Dummy log in order to mantain consistency although Sentry has no close() method
			self.Logger.Info("Closing Sentry service")
			self.Logger.Info("Closed Sentry service")
		}

		err = self.Logger.Close(ctx)
		if err != nil {
			return Errors.ErrObserverGeneric().WrapAs(err)
		}

		self.Logger.Info("Closed observer")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrObserverTimedOut()
	default:
		return Errors.ErrObserverGeneric().Wrap(err)
	}
}
