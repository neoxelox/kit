package kit

import (
	"github.com/labstack/echo/v4"
)

type BinderConfig struct {
}

type Binder struct {
	binder echo.DefaultBinder
	config BinderConfig
	logger Logger
}

func NewBinder(logger Logger, config BinderConfig) *Binder {
	logger.SetFile()

	return &Binder{
		logger: logger,
		config: config,
		binder: echo.DefaultBinder{},
	}
}

func (self *Binder) Bind(i interface{}, c echo.Context) error {
	if err := self.binder.Bind(i, c); err != nil {
		return Errors.ErrBinderGeneric().Wrap(err)
	}

	return nil
}
