package middleware

import (
	"context"
	"reflect"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

// TODO: dump request/response body, params and headers for easy debug tracing in logs

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
		traceID := self.observer.GetTrace(traceCtx).String()

		ctx.Response().Header().Set(_OBSERVER_MIDDLEWARE_RESPONSE_TRACE_ID_HEADER, traceID)
		ctx.SetRequest(ctx.Request().WithContext(traceCtx))

		err := next(ctx)

		request := ctx.Request()
		response := ctx.Response()

		// TODO: find another cleaner way to do this
		// Patch in order to be able to have better path info in sentry
		// now that the router has been executed
		sentryHub := sentry.GetHubFromContext(request.Context())
		if sentryHub != nil {
			sentryHub.Scope().SetTransaction(ctx.Path())
		}
		// -----

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
		traceID := self.observer.GetTrace(ctx).String()

		err := next.ProcessTask(ctx, task)

		// TODO: find another way to get task queue without using reflect
		// TODO: find a way to get the real task execution state
		qname := reflect.ValueOf(task.ResultWriter()).Elem().FieldByName("qname")
		queue := "unknown"
		if qname.String() != "" {
			queue = qname.String()
		}

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

		return err // nolint: wrapcheck
	})
}
