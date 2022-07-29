package kit

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/getsentry/sentry-go"
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
	Environment  _environment
	Release      string
	AppName      string
	Level        _level
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
		AppName: config.AppName,
		Level:   config.Level,
	})

	_, file, _, _ := runtime.Caller(0)

	if config.SentryConfig != nil {
		// TODO: only retry on specific errors
		err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
			return Utils.ExponentialRetry(
				config.RetryConfig.Attempts, config.RetryConfig.InitialDelay, config.RetryConfig.LimitDelay,
				nil, func(attempt int) error {
					logger.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).
						Msgf("Trying to connect to the Sentry service %d/%d", attempt, config.RetryConfig.Attempts)

					err := sentry.Init(sentry.ClientOptions{
						Dsn:              config.SentryConfig.Dsn,
						Environment:      string(config.Environment),
						Release:          config.Release,
						ServerName:       config.AppName,
						Debug:            false,
						AttachStacktrace: false, // Already done by errors package
						SampleRate:       1.0,   // Error events
						TracesSampleRate: 0,     // Transaction events. TODO: activate?
					})
					if err != nil {
						return ErrObserverGeneric().WrapAs(err)
					}

					return nil
				})
		})
		switch {
		case err == nil:
		case ErrDeadlineExceeded().Is(err):
			return nil, ErrObserverTimedOut()
		default:
			return nil, ErrObserverGeneric().Wrap(err)
		}

		logger.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Connected to the Sentry service")
	}

	return &Observer{
		config: config,
		Logger: *logger,
	}, nil
}

func (self *Observer) Anchor() {
	self.Logger.SetFile(1)
}

func (self Observer) Error(i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

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
			return ErrObserverGeneric().WrapAs(err)
		}

		if self.config.SentryConfig != nil {
			sentryFlushTimeout := _OBSERVER_DEFAULT_SENTRY_FLUSH_TIMEOUT
			if ctxDeadline, ok := ctx.Deadline(); ok {
				sentryFlushTimeout = time.Until(ctxDeadline)
			}

			ok := sentry.Flush(sentryFlushTimeout)
			if !ok {
				return ErrObserverGeneric().With("sentry lost events while flushing")
			}
		}

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrObserverTimedOut()
	default:
		return ErrObserverGeneric().Wrap(err)
	}
}

func (self Observer) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		_, file, _, _ := runtime.Caller(0)

		self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Closing observer")

		err := self.Flush(ctx)
		if err != nil {
			return ErrObserverGeneric().WrapAs(err)
		}

		if self.config.SentryConfig != nil {
			// Dummy log in order to mantain consistency although Sentry has no close() method
			self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Closing Sentry service")
			self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Closed Sentry service")
		}

		err = self.Logger.Close(ctx)
		if err != nil {
			return ErrObserverGeneric().WrapAs(err)
		}

		self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Closed observer")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrObserverTimedOut()
	default:
		return ErrObserverGeneric().Wrap(err)
	}
}
