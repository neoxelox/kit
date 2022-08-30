package middleware

import (
	"time"

	"github.com/getsentry/sentry-go"
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
	kit.Middleware
	config   ObserverConfig
	observer kit.Observer
}

func NewObserver(observer kit.Observer, config ObserverConfig) *Observer {
	return &Observer{
		config:   config,
		observer: observer,
	}
}

func (self *Observer) Handle(next echo.HandlerFunc) echo.HandlerFunc {
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
