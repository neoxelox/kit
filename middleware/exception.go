package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

// TODO: See how to improve this, as ctx.Error() should be called right after the actual handler
// (so in theory the ExceptionMiddleware should be the first middleware after the handler)

type ExceptionConfig struct {
}

type Exception struct {
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
			// Handle, serialize and write exception response
			ctx.Error(err)
		}

		return err
	}
}
