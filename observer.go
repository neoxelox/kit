package kit

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/getsentry/sentry-go"
	"github.com/neoxelox/gilk"
	"github.com/rs/xid"
)

// TODO: fix caller func name ".func1"?
// TODO: fix unnamed Sentry spans
// TODO: fix path params in transaction names (see observer middleware patch)
// TODO: add NewRelic for APM and events
// TODO: add OpenTelemetry for tracing instead of Sentry/NewRelic?

const (
	_OBSERVER_REQUEST_TRACE_ID_HEADER = "X-Trace-Id"
	_OBSERVER_SENTRY_TRACE_ID_TAG     = "trace_id"
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
						TracesSampleRate: 0.2,   // Transaction events
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

func (self Observer) Print(ctx context.Context, i ...interface{}) { // nolint
	if !(LvlTrace >= self.config.Level) {
		return
	}

	self.Logger.Print(i...)
}

func (self Observer) Printf(ctx context.Context, format string, i ...interface{}) { // nolint
	if !(LvlTrace >= self.config.Level) {
		return
	}

	self.Logger.Printf(format, i...)
}

func (self Observer) Debug(ctx context.Context, i ...interface{}) { // nolint
	if !(LvlDebug >= self.config.Level) {
		return
	}

	self.Logger.Debug(i...)
}

func (self Observer) Debugf(ctx context.Context, format string, i ...interface{}) { // nolint
	if !(LvlDebug >= self.config.Level) {
		return
	}

	self.Logger.Debugf(format, i...)
}

func (self Observer) Info(ctx context.Context, i ...interface{}) { // nolint
	if !(LvlInfo >= self.config.Level) {
		return
	}

	self.Logger.Info(i...)
}

func (self Observer) Infof(ctx context.Context, format string, i ...interface{}) { // nolint
	if !(LvlInfo >= self.config.Level) {
		return
	}

	self.Logger.Infof(format, i...)
}

func (self Observer) Warn(ctx context.Context, i ...interface{}) { // nolint
	if !(LvlWarn >= self.config.Level) {
		return
	}

	self.Logger.Warn(i...)
}

func (self Observer) Warnf(ctx context.Context, format string, i ...interface{}) { // nolint
	if !(LvlWarn >= self.config.Level) {
		return
	}

	self.Logger.Warnf(format, i...)
}

func (self Observer) sendErrToSentry(ctx context.Context, i ...interface{}) {
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

	sentryHub := sentry.GetHubFromContext(ctx)
	if sentryHub == nil {
		sentryHub = sentry.CurrentHub().Clone()
	}

	sentryHub.CaptureEvent(sentryEvent)
}

func (self Observer) Error(ctx context.Context, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Error(i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(ctx, i...)
	}
}

func (self Observer) Errorf(ctx context.Context, format string, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Errorf(format, i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(ctx, fmt.Sprintf(format, i...))
	}
}

func (self Observer) Fatal(ctx context.Context, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Fatal(i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(ctx, i...)
	}
}

func (self Observer) Fatalf(ctx context.Context, format string, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Fatalf(format, i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(ctx, fmt.Sprintf(format, i...))
	}
}

func (self Observer) Panic(ctx context.Context, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Panic(i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(ctx, i...)
	}
}

func (self Observer) Panicf(ctx context.Context, format string, i ...interface{}) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Panicf(format, i...)

	if self.config.SentryConfig != nil {
		self.sendErrToSentry(ctx, fmt.Sprintf(format, i...))
	}
}

func (self Observer) WithLevel(ctx context.Context, level _level, i ...interface{}) {
	switch level {
	case LvlTrace:
		self.Print(ctx, i...)
	case LvlDebug:
		self.Debug(ctx, i...)
	case LvlInfo:
		self.Info(ctx, i...)
	case LvlWarn:
		self.Warn(ctx, i...)
	case LvlError:
		self.Error(ctx, i...)
	}
}

func (self Observer) WithLevelf(ctx context.Context, level _level, format string, i ...interface{}) {
	switch level {
	case LvlTrace:
		self.Printf(ctx, format, i...)
	case LvlDebug:
		self.Debugf(ctx, format, i...)
	case LvlInfo:
		self.Infof(ctx, format, i...)
	case LvlWarn:
		self.Warnf(ctx, format, i...)
	case LvlError:
		self.Errorf(ctx, format, i...)
	}
}

// TODO
func (self Observer) Metric() {
}

func (self Observer) SetTrace(ctx context.Context, traceID xid.ID) context.Context {
	return context.WithValue(ctx, KeyTraceID, traceID)
}

func (self Observer) GetTrace(ctx context.Context) xid.ID {
	if ctxTraceID, ok := ctx.Value(KeyTraceID).(xid.ID); ok {
		return ctxTraceID
	}

	return xid.New()
}

func (self Observer) TraceSpan(ctx context.Context, name ...string) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	ctx = self.SetTrace(ctx, traceID)

	pc, _, _, _ := runtime.Caller(1)
	caller := runtime.FuncForPC(pc).Name()
	spanName := caller
	if len(name) > 0 { // nolint
		spanName = name[0]
	}

	var sentrySpan *sentry.Span
	if self.config.SentryConfig != nil { // nolint
		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
		}

		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID.String())
		if sentryHub.Scope().Transaction() == "" { // nolint
			sentryHub.Scope().SetTransaction(caller)
		}

		ctx = sentry.SetHubOnContext(ctx, sentryHub)

		sentrySpan = sentry.StartSpan(ctx, spanName)
		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.SentryConfig != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) TraceRequest(ctx context.Context, request *http.Request) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	traceIDRaw := request.Header.Get(_OBSERVER_REQUEST_TRACE_ID_HEADER)
	if traceIDRaw != "" { // nolint
		traceID, _ = xid.FromString(traceIDRaw) // nolint
	}
	ctx = self.SetTrace(ctx, traceID)

	spanName := request.RequestURI

	var endGilkRequest func()
	if self.config.GilkConfig != nil { // nolint
		ctx, endGilkRequest = gilk.NewContext(ctx, request.RequestURI, request.Method)
	}

	var sentrySpan *sentry.Span
	if self.config.SentryConfig != nil { // nolint
		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
		}

		sentryHub.Scope().SetRequest(request)
		sentryHub.Scope().SetUser(sentry.User{
			IPAddress: request.RemoteAddr,
		})
		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID.String())
		if sentryHub.Scope().Transaction() == "" { // nolint
			sentryHub.Scope().SetTransaction(spanName)
		}

		ctx = sentry.SetHubOnContext(ctx, sentryHub)

		sentrySpan = sentry.StartSpan(ctx, spanName, sentry.ContinueFromRequest(request))
		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.GilkConfig != nil {
			endGilkRequest()
		}

		if self.config.SentryConfig != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) TraceQuery(ctx context.Context, sql string, args ...interface{}) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	ctx = self.SetTrace(ctx, traceID)

	// Skip 2 dataframes when using observer in database wrapper, otherwise 1
	pc, _, _, _ := runtime.Caller(2)
	caller := runtime.FuncForPC(pc).Name()
	spanName := caller

	var endGilkQuery func()
	if self.config.GilkConfig != nil { // nolint
		dArgs := make([]interface{}, len(args))

		copy(dArgs, args)

		ctx, endGilkQuery = gilk.NewQuery(ctx, sql, dArgs...)
	}

	var sentrySpan *sentry.Span
	if self.config.SentryConfig != nil { // nolint
		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
		}

		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID.String())
		if sentryHub.Scope().Transaction() == "" { // nolint
			sentryHub.Scope().SetTransaction(caller)
		}

		ctx = sentry.SetHubOnContext(ctx, sentryHub)

		sentrySpan = sentry.StartSpan(ctx, spanName)
		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.GilkConfig != nil {
			endGilkQuery()
		}

		if self.config.SentryConfig != nil {
			sentrySpan.Finish()
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
