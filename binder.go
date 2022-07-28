package kit

import (
	"github.com/labstack/echo/v4"
)

type BinderConfig struct {
}

type Binder struct {
	binder   echo.DefaultBinder
	config   BinderConfig
	observer Observer
}

func NewBinder(observer Observer, config BinderConfig) *Binder {
	observer.Anchor()

	return &Binder{
		observer: observer,
		config:   config,
		binder:   echo.DefaultBinder{},
	}
}

func (self *Binder) Bind(i interface{}, c echo.Context) error {
	if err := self.binder.Bind(i, c); err != nil {
		return ErrBinderGeneric().Wrap(err)
	}

	return nil
}
