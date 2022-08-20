package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

type ExceptionConfig struct {
}

type Exception struct {
	kit.Middleware
	config   ExceptionConfig
	observer kit.Observer
}

func NewException(observer kit.Observer, config ExceptionConfig) *Exception {
	return &Exception{
		config:   config,
		observer: observer,
	}
}

func (self *Exception) Handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		err := next(ctx)
		if err != nil {
			// If request was already committed it means another middleware or an actual view
			// has already called the exception handler or written an appropriate response,
			// but we should send the error upwards to cover the case the handler timed out.
			if ctx.Response().Committed {
				return err
			}

			// Handle, serialize and write exception response
			ctx.Error(err)
		}

		return nil
	}
}
