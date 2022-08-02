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
	observer.Anchor()

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

		return nil
	}
}
