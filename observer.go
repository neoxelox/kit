package kit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/getsentry/sentry-go"
	"github.com/neoxelox/gilk"
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

type ObserverGilkConfig struct {
	Port int
}

type ObserverConfig struct {
	Environment  _environment
	Release      string
	AppName      string
	Level        _level
	SentryConfig *ObserverSentryConfig
	GilkConfig   *ObserverGilkConfig
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
		AppName:        config.AppName,
		Level:          config.Level,
		SkipFrameCount: ptr(2),
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

		logger.Info("Connected to the Sentry service")
	}

	if config.GilkConfig != nil {
		logger.Info("Starting the Gilk service")

		// Skip 3 dataframes when using observer in database wrapper, otherwise 2
		gilk.SkippedStackFrames = 3

		go func() {
			err := gilk.Serve(fmt.Sprintf(":%d", config.GilkConfig.Port))
			if err != nil && err != http.ErrServerClosed {
				logger.Error(ErrObserverGeneric().Wrap(err))
			}
		}()

		logger.Infof("Started the Gilk service at port %d", config.GilkConfig.Port)
	}

	return &Observer{
		config: config,
		Logger: *logger,
	}, nil
}

func (self Observer) Warn(i ...interface{}) {
	// Observer must wrap Warn because of skip frame count on caller
	self.Logger.Warn(i...)
}

func (self Observer) Warnf(format string, i ...interface{}) {
	// Observer must wrap Warnf because of skip frame count on caller
	self.Logger.Warnf(format, i...)
}

func (self Observer) sendErrToSentry(i ...interface{}) {
	if len(i) == 0 {
		return
	}

	var sentryEvent *sentry.Event
	var sentryEventExtra map[string]interface{}

	switch err := i[0].(type) {
	case nil:
		return
	case *Error:
		sentryEvent, sentryEventExtra = errors.BuildSentryReport(err.Unwrap())
	case *Exception:
		sentryEvent, sentryEventExtra = errors.BuildSentryReport(err.Unwrap())
	case error:
		sentryEvent, sentryEventExtra = errors.BuildSentryReport(err)
	default:
		sentryEvent, sentryEventExtra = errors.BuildSentryReport(errors.NewWithDepth(2, fmt.Sprint(i...)))
	}

	for k, v := range sentryEventExtra {
		sentryEvent.Extra[k] = v
	}

	sentryEvent.Level = sentry.LevelError

	// TODO: enhance exception message and title

	sentry.CaptureEvent(sentryEvent)
}

func (self Observer) Error(i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Error(i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(i...)
	}
}

func (self Observer) Errorf(format string, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Errorf(format, i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(fmt.Sprintf(format, i...))
	}
}

func (self Observer) Fatal(i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Fatal(i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(i...)
	}
}

func (self Observer) Fatalf(format string, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Fatalf(format, i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(fmt.Sprintf(format, i...))
	}
}

func (self Observer) Panic(i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Panic(i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(i...)
	}
}

func (self Observer) Panicf(format string, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Panicf(format, i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(fmt.Sprintf(format, i...))
	}
}

// TODO
func (self Observer) Metric() {

}

// TODO
func (self Observer) Trace(ctx context.Context) (context.Context, func()) {
	return ctx, func() {}
}

func (self Observer) TraceRequest(ctx context.Context, request *http.Request) (context.Context, func()) {
	var endGilkRequest func()

	if self.config.GilkConfig != nil {
		ctx, endGilkRequest = gilk.NewContext(ctx, request.URL.Path, request.Method)
	}

	return ctx, func() {
		if self.config.GilkConfig != nil {
			endGilkRequest()
		}
	}
}

func (self Observer) TraceQuery(ctx context.Context, sql string, args ...interface{}) (context.Context, func()) {
	var endGilkQuery func()

	if self.config.GilkConfig != nil {
		dArgs := make([]interface{}, len(args))

		copy(dArgs, args)

		ctx, endGilkQuery = gilk.NewQuery(ctx, sql, dArgs...)
	}

	return ctx, func() {
		if self.config.GilkConfig != nil {
			endGilkQuery()
		}
	}
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

		if self.config.GilkConfig != nil {
			gilk.Reset()
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
		self.Logger.Info("Closing observer")

		err := self.Flush(ctx)
		if err != nil {
			return ErrObserverGeneric().WrapAs(err)
		}

		if self.config.SentryConfig != nil {
			// Dummy log in order to mantain consistency although Sentry has no close() method
			self.Logger.Info("Closing Sentry service")
			self.Logger.Info("Closed Sentry service")
		}

		if self.config.GilkConfig != nil {
			// Dummy log in order to mantain consistency although Gilk has no close() method
			self.Logger.Info("Closing Gilk service")
			self.Logger.Info("Closed Gilk service")
		}

		err = self.Logger.Close(ctx)
		if err != nil {
			return ErrObserverGeneric().WrapAs(err)
		}

		self.Logger.Info("Closed observer")

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
