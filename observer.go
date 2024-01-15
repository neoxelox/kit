package kit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hibiken/asynq"
	"github.com/mkideal/cli"
	"github.com/neoxelox/errors"
	"github.com/neoxelox/gilk"
	"github.com/rs/xid"

	"github.com/neoxelox/kit/util"
)

const (
	_OBSERVER_REQUEST_TRACE_ID_HEADER = "X-Trace-Id"
	_OBSERVER_TASK_TRACE_ID_HEADER    = "x_trace_id"
	_OBSERVER_SENTRY_TRACE_ID_TAG     = "trace_id"
	_OBSERVER_SENTRY_FLUSH_TIMEOUT    = 5 * time.Second
)

var (
	KeyTraceID Key = KeyBase + "trace:id"
)

var (
	ErrObserverGeneric  = errors.New("observer failed")
	ErrObserverTimedOut = errors.New("observer timed out")
)

var (
	_OBSERVER_DEFAULT_CONFIG = ObserverConfig{
		Sentry: nil,
		Gilk:   nil,
	}

	_OBSERVER_DEFAULT_RETRY_CONFIG = RetryConfig{
		Attempts:     1,
		InitialDelay: 0 * time.Second,
		LimitDelay:   0 * time.Second,
		Retriables:   []error{},
	}
)

type ObserverSentryConfig struct {
	Dsn string
}

type ObserverGilkConfig struct {
	Port int
}

type ObserverConfig struct {
	Environment Environment
	Release     string
	Service     string
	Level       Level
	Sentry      *ObserverSentryConfig
	Gilk        *ObserverGilkConfig
}

type Observer struct {
	config ObserverConfig
	Logger
}

func NewObserver(ctx context.Context, config ObserverConfig, retry ...RetryConfig) (*Observer, error) {
	util.Merge(&config, _OBSERVER_DEFAULT_CONFIG)
	_retry := util.Optional(retry, _OBSERVER_DEFAULT_RETRY_CONFIG)

	logger := NewLogger(LoggerConfig{
		Service:        config.Service,
		Level:          config.Level,
		SkipFrameCount: util.Pointer(2),
	})

	if config.Sentry != nil {
		err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
			return util.ExponentialRetry(
				_retry.Attempts, _retry.InitialDelay, _retry.LimitDelay,
				_retry.Retriables, func(attempt int) error {
					logger.Infof("Trying to connect to the Sentry service %d/%d", attempt, _retry.Attempts)

					err := sentry.Init(sentry.ClientOptions{
						Dsn:                config.Sentry.Dsn,
						Environment:        string(config.Environment),
						Release:            config.Release,
						ServerName:         config.Service,
						Debug:              false,
						AttachStacktrace:   false, // Already done by errors package
						EnableTracing:      true,
						SampleRate:         1.0,  // Error events
						TracesSampleRate:   0.25, // Transaction events
						ProfilesSampleRate: 1.0,  // Profiling events out of Transaction events
					})
					if err != nil {
						return ErrObserverGeneric.Raise().Cause(err)
					}

					return nil
				})
		})
		if err != nil {
			if util.ErrDeadlineExceeded.Is(err) {
				return nil, ErrObserverTimedOut.Raise().Cause(err)
			}

			return nil, err
		}

		logger.Info("Connected to the Sentry service")
	}

	if config.Gilk != nil {
		logger.Info("Starting the Gilk service")

		// Skip 3 dataframes when using observer in database wrapper, otherwise 2
		gilk.SkippedStackFrames = 3

		go func() {
			err := gilk.Serve(fmt.Sprintf(":%d", config.Gilk.Port))
			if err != nil && err != http.ErrServerClosed {
				logger.Error(ErrObserverGeneric.Raise().Cause(err))
			}
		}()

		logger.Infof("Started the Gilk service at port %d", config.Gilk.Port)
	}

	return &Observer{
		config: config,
		Logger: *logger,
	}, nil
}

func (self Observer) Print(ctx context.Context, i ...any) {
	if !(LvlTrace >= self.config.Level) {
		return
	}

	self.Logger.Print(i...)
}

func (self Observer) Printf(ctx context.Context, format string, i ...any) {
	if !(LvlTrace >= self.config.Level) {
		return
	}

	self.Logger.Printf(format, i...)
}

func (self Observer) Debug(ctx context.Context, i ...any) {
	if !(LvlDebug >= self.config.Level) {
		return
	}

	self.Logger.Debug(i...)
}

func (self Observer) Debugf(ctx context.Context, format string, i ...any) {
	if !(LvlDebug >= self.config.Level) {
		return
	}

	self.Logger.Debugf(format, i...)
}

func (self Observer) Info(ctx context.Context, i ...any) {
	if !(LvlInfo >= self.config.Level) {
		return
	}

	self.Logger.Info(i...)
}

func (self Observer) Infof(ctx context.Context, format string, i ...any) {
	if !(LvlInfo >= self.config.Level) {
		return
	}

	self.Logger.Infof(format, i...)
}

func (self Observer) Warn(ctx context.Context, i ...any) {
	if !(LvlWarn >= self.config.Level) {
		return
	}

	self.Logger.Warn(i...)
}

func (self Observer) Warnf(ctx context.Context, format string, i ...any) {
	if !(LvlWarn >= self.config.Level) {
		return
	}

	self.Logger.Warnf(format, i...)
}

func (self Observer) sendErrorToSentry(ctx context.Context, i ...any) {
	if len(i) == 0 {
		return
	}

	sentryHub := sentry.GetHubFromContext(ctx)
	if sentryHub == nil {
		sentryHub = sentry.CurrentHub().Clone()
	}

	switch err := i[0].(type) {
	case errors.Error:
		sentryHub.CaptureEvent(err.SentryReport())
	case *errors.Error:
		sentryHub.CaptureEvent(err.SentryReport())
	case HTTPError:
		switch err := err.Unwrap().(type) {
		case errors.Error:
			sentryHub.CaptureEvent(err.SentryReport())
		case *errors.Error:
			sentryHub.CaptureEvent(err.SentryReport())
		case nil:
			// Ignore
		default:
			sentryHub.CaptureException(err)
		}
	case *HTTPError:
		switch err := err.Unwrap().(type) {
		case errors.Error:
			sentryHub.CaptureEvent(err.SentryReport())
		case *errors.Error:
			sentryHub.CaptureEvent(err.SentryReport())
		case nil:
			// Ignore
		default:
			sentryHub.CaptureException(err)
		}
	case nil:
		// Ignore
	case error:
		sentryHub.CaptureException(err)
	default:
		sentryHub.CaptureException(fmt.Errorf("%v", err))
	}
}

func (self Observer) Error(ctx context.Context, i ...any) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Error(i...)

	if self.config.Sentry != nil {
		self.sendErrorToSentry(ctx, i...)
	}
}

func (self Observer) Errorf(ctx context.Context, format string, i ...any) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Errorf(format, i...)

	if self.config.Sentry != nil {
		self.sendErrorToSentry(ctx, fmt.Sprintf(format, i...))
	}
}

func (self Observer) Fatal(ctx context.Context, i ...any) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Fatal(i...)

	if self.config.Sentry != nil {
		self.sendErrorToSentry(ctx, i...)
	}
}

func (self Observer) Fatalf(ctx context.Context, format string, i ...any) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Fatalf(format, i...)

	if self.config.Sentry != nil {
		self.sendErrorToSentry(ctx, fmt.Sprintf(format, i...))
	}
}

func (self Observer) Panic(ctx context.Context, i ...any) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Panic(i...)

	if self.config.Sentry != nil {
		self.sendErrorToSentry(ctx, i...)
	}
}

func (self Observer) Panicf(ctx context.Context, format string, i ...any) {
	if !(LvlError >= self.config.Level) {
		return
	}

	self.Logger.Panicf(format, i...)

	if self.config.Sentry != nil {
		self.sendErrorToSentry(ctx, fmt.Sprintf(format, i...))
	}
}

func (self Observer) WithLevel(ctx context.Context, level Level, i ...any) {
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

func (self Observer) WithLevelf(ctx context.Context, level Level, format string, i ...any) {
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

func (self Observer) SetTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, KeyTraceID, traceID)
}

func (self Observer) GetTrace(ctx context.Context) string {
	if ctxTraceID, ok := ctx.Value(KeyTraceID).(string); ok {
		return ctxTraceID
	}

	return xid.New().String()
}

func (self Observer) TraceSpan(ctx context.Context, name ...string) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	ctx = self.SetTrace(ctx, traceID)

	pc, _, _, _ := runtime.Caller(1)
	spanName := util.Optional(name, runtime.FuncForPC(pc).Name())

	var sentrySpan *sentry.Span
	if self.config.Sentry != nil {
		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, sentryHub)
		}

		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID)

		if sentry.TransactionFromContext(ctx) == nil {
			sentrySpan = sentry.StartTransaction(
				ctx, spanName, sentry.WithOpName(spanName), sentry.WithTransactionSource(sentry.SourceComponent))
		} else {
			sentrySpan = sentry.StartSpan(ctx, spanName)
		}

		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.Sentry != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) TraceRequest(ctx context.Context, request *http.Request) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	if request.Header.Get(_OBSERVER_REQUEST_TRACE_ID_HEADER) != "" {
		traceID = request.Header.Get(_OBSERVER_REQUEST_TRACE_ID_HEADER)
	}
	ctx = self.SetTrace(ctx, traceID)

	spanName := fmt.Sprintf("%s %s", request.Method, request.RequestURI)

	var endGilkRequest func()
	if self.config.Gilk != nil {
		ctx, endGilkRequest = gilk.NewContext(ctx, request.RequestURI, request.Method)
	}

	var sentrySpan *sentry.Span
	if self.config.Sentry != nil {
		sentryTrace := ""
		if request.Header.Get(sentry.SentryTraceHeader) != "" {
			sentryTrace = request.Header.Get(sentry.SentryTraceHeader)
		}

		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, sentryHub)
		}

		sentryHub.Scope().SetRequest(request)
		sentryHub.Scope().SetUser(sentry.User{
			IPAddress: request.RemoteAddr,
		})
		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID)

		if sentry.TransactionFromContext(ctx) == nil {
			sentrySpan = sentry.StartTransaction(ctx, spanName, sentry.WithOpName(spanName),
				sentry.WithTransactionSource(sentry.SourceURL), sentry.ContinueFromTrace(sentryTrace))
		} else {
			sentrySpan = sentry.StartSpan(ctx, spanName, sentry.ContinueFromTrace(sentryTrace))
		}

		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.Gilk != nil {
			endGilkRequest()
		}

		if self.config.Sentry != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) TraceQuery(ctx context.Context, sql string, args ...any) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	ctx = self.SetTrace(ctx, traceID)

	// Skip 2 dataframes when using observer in database wrapper, otherwise 1
	pc, _, _, _ := runtime.Caller(2)
	spanName := runtime.FuncForPC(pc).Name()

	var endGilkQuery func()
	if self.config.Gilk != nil {
		dArgs := make([]any, len(args))
		copy(dArgs, args)

		ctx, endGilkQuery = gilk.NewQuery(ctx, sql, dArgs...)
	}

	var sentrySpan *sentry.Span
	if self.config.Sentry != nil {
		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, sentryHub)
		}

		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID)

		if sentry.TransactionFromContext(ctx) == nil {
			sentrySpan = sentry.StartTransaction(
				ctx, spanName, sentry.WithOpName(spanName), sentry.WithTransactionSource(sentry.SourceComponent))
		} else {
			sentrySpan = sentry.StartSpan(ctx, spanName)
		}

		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.Gilk != nil {
			endGilkQuery()
		}

		if self.config.Sentry != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) TraceTask(ctx context.Context, task *asynq.Task) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	var data map[string]any
	_ = json.Unmarshal(task.Payload(), &data)
	if data[_OBSERVER_TASK_TRACE_ID_HEADER] != nil {
		traceID = data[_OBSERVER_TASK_TRACE_ID_HEADER].(string)
	}
	ctx = self.SetTrace(ctx, traceID)

	spanName := task.Type()

	var sentrySpan *sentry.Span
	if self.config.Sentry != nil {
		sentryTrace := ""
		if data[sentry.SentryTraceHeader] != nil {
			sentryTrace = data[sentry.SentryTraceHeader].(string)
		}

		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, sentryHub)
		}

		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID)

		if sentry.TransactionFromContext(ctx) == nil {
			sentrySpan = sentry.StartTransaction(ctx, spanName, sentry.WithOpName(spanName),
				sentry.WithTransactionSource(sentry.SourceTask), sentry.ContinueFromTrace(sentryTrace))
		} else {
			sentrySpan = sentry.StartSpan(ctx, spanName, sentry.ContinueFromTrace(sentryTrace))
		}

		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.Sentry != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) TraceCommand(ctx context.Context, command *cli.Context) (context.Context, func()) {
	traceID := self.GetTrace(ctx)
	ctx = self.SetTrace(ctx, traceID)

	spanName := fmt.Sprintf("$ %s", command.Path())

	var sentrySpan *sentry.Span
	if self.config.Sentry != nil {
		sentryHub := sentry.GetHubFromContext(ctx)
		if sentryHub == nil {
			sentryHub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, sentryHub)
		}

		sentryHub.Scope().SetTag(_OBSERVER_SENTRY_TRACE_ID_TAG, traceID)

		if sentry.TransactionFromContext(ctx) == nil {
			sentrySpan = sentry.StartTransaction(
				ctx, spanName, sentry.WithOpName(spanName), sentry.WithTransactionSource(sentry.SourceComponent))
		} else {
			sentrySpan = sentry.StartSpan(ctx, spanName)
		}

		ctx = sentrySpan.Context()
	}

	return ctx, func() {
		if self.config.Sentry != nil {
			sentrySpan.Finish()
		}
	}
}

func (self Observer) Flush(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		err := self.Logger.Flush(ctx)
		if err != nil {
			return err
		}

		if self.config.Sentry != nil {
			sentryFlushTimeout := _OBSERVER_SENTRY_FLUSH_TIMEOUT
			if ctxDeadline, ok := ctx.Deadline(); ok {
				sentryFlushTimeout = time.Until(ctxDeadline)
			}

			ok := sentry.Flush(sentryFlushTimeout)
			if !ok {
				return ErrObserverGeneric.Raise().With("sentry lost events while flushing")
			}
		}

		if self.config.Gilk != nil {
			gilk.Reset()
		}

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrObserverTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}

func (self Observer) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.Logger.Info("Closing observer")

		err := self.Flush(ctx)
		if err != nil {
			return err
		}

		if self.config.Sentry != nil {
			// Dummy log in order to mantain consistency although Sentry has no close() method
			self.Logger.Info("Closing Sentry service")
			self.Logger.Info("Closed Sentry service")
		}

		if self.config.Gilk != nil {
			// Dummy log in order to mantain consistency although Gilk has no close() method
			self.Logger.Info("Closing Gilk service")
			self.Logger.Info("Closed Gilk service")
		}

		err = self.Logger.Close(ctx)
		if err != nil {
			return err
		}

		self.Logger.Info("Closed observer")

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrObserverTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}
