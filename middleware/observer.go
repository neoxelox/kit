package middleware

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

const (
	_OBSERVER_MIDDLEWARE_RESPONSE_TRACE_ID_HEADER = "X-Trace-Id"
)

type ObserverConfig struct {
}

type Observer struct {
	config   ObserverConfig
	observer kit.Observer
}

func NewObserver(observer kit.Observer, config ObserverConfig) *Observer {
	return &Observer{
		config:   config,
		observer: observer,
	}
}

func (self *Observer) HandleRequest(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		start := time.Now()

		traceCtx, endTraceRequest := self.observer.TraceRequest(ctx.Request().Context(), ctx.Request())
		defer endTraceRequest()

		ctx.SetRequest(ctx.Request().WithContext(traceCtx))
		traceID := self.observer.GetTrace(traceCtx)
		sentrySpan := sentry.SpanFromContext(traceCtx)

		ctx.Response().Header().Set(_OBSERVER_MIDDLEWARE_RESPONSE_TRACE_ID_HEADER, traceID)
		if sentrySpan != nil {
			ctx.Response().Header().Set(sentry.SentryTraceHeader, sentrySpan.ToSentryTrace())
		}

		err := next(ctx)

		request := ctx.Request()
		response := ctx.Response()

		// Overwrite the Sentry transaction name now that the router
		// has been executed to have better path aggregation
		sentryTx := sentry.TransactionFromContext(request.Context())
		if sentryTx != nil {
			sentryTx.Name = fmt.Sprintf("%s %s", request.Method, ctx.Path())
			sentryTx.Source = sentry.SourceRoute
		}

		stop := time.Now()

		self.observer.Logger.Logger().Info().
			Str("host", request.Host).
			Str("method", request.Method).
			Str("path", request.RequestURI).
			Int("status", response.Status).
			Str("ip_address", request.RemoteAddr).
			Dur("latency", stop.Sub(start)).
			Str("trace_id", traceID).
			Msg("")

		return err
	}
}

func (self *Observer) HandleTask(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()

		ctx, endTraceTask := self.observer.TraceTask(ctx, task)
		defer endTraceTask()

		traceID := self.observer.GetTrace(ctx)

		err := next.ProcessTask(ctx, task)

		// TODO: find a way to get task queue without using reflect
		qname := reflect.ValueOf(task.ResultWriter()).Elem().FieldByName("qname")
		queue := "unknown"
		if qname.String() != "" {
			queue = qname.String()
		}

		// TODO: find a way to get the real task execution state
		status := "succeeded"
		if err != nil {
			status = "failed"
		}

		stop := time.Now()

		self.observer.Logger.Logger().Info().
			Str("queue", queue).
			Str("type", task.Type()).
			Str("status", status).
			Dur("latency", stop.Sub(start)).
			Str("trace_id", traceID).
			Msg("")

		return err
	})
}
