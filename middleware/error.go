package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
	"github.com/neoxelox/kit/util"
)

var (
	_ERROR_MIDDLEWARE_DEFAULT_CONFIG = ErrorConfig{}
)

type ErrorConfig struct {
}

type Error struct {
	config   ErrorConfig
	observer *kit.Observer
}

func NewError(observer *kit.Observer, config ErrorConfig) *Error {
	util.Merge(&config, _ERROR_MIDDLEWARE_DEFAULT_CONFIG)

	return &Error{
		config:   config,
		observer: observer,
	}
}

func (self *Error) Handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		err := next(ctx)
		if err != nil {
			// Force pass error to the error handler to serialize and write error response
			ctx.Error(err)
		}

		return err
	}
}
