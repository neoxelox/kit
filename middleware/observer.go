package middleware

import (
	"time"

	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

// TODO: dump request/response body, params and headers for easy debug tracing in logs
// TODO: set request to sentry scope

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

		ctx.SetRequest(ctx.Request().WithContext(traceCtx))

		request := ctx.Request()

		next(ctx) // nolint

		response := ctx.Response()

		stop := time.Now()

		self.observer.Logger.Logger().Info().
			Str("host", request.Host).
			Str("method", request.Method).
			Str("path", request.RequestURI).
			Int("status", response.Status).
			Str("ip_address", ctx.RealIP()).
			Dur("latency", stop.Sub(start)).
			Msg("")

		return nil
	}
}
