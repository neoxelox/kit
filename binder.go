package kit

import (
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit/util"
)

var (
	_BINDER_DEFAULT_CONFIG = BinderConfig{}
)

type BinderConfig struct {
}

type Binder struct {
	binder   *echo.DefaultBinder
	config   BinderConfig
	observer Observer
}

func NewBinder(observer Observer, config BinderConfig) *Binder {
	util.Merge(&config, _BINDER_DEFAULT_CONFIG)

	return &Binder{
		observer: observer,
		config:   config,
		binder:   &echo.DefaultBinder{},
	}
}

func (self *Binder) Bind(i any, c echo.Context) error {
	if err := self.binder.Bind(i, c); err != nil {
		return ErrBinderGeneric().Wrap(err)
	}

	return nil
}
