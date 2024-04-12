package kit

import (
	"github.com/labstack/echo/v4"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

var (
	ErrBinderGeneric = errors.New("binder failed")
)

var (
	_BINDER_DEFAULT_CONFIG = BinderConfig{}
)

type BinderConfig struct {
}

type Binder struct {
	config   BinderConfig
	observer *Observer
	binder   *echo.DefaultBinder
}

func NewBinder(observer *Observer, config BinderConfig) *Binder {
	util.Merge(&config, _BINDER_DEFAULT_CONFIG)

	return &Binder{
		observer: observer,
		config:   config,
		binder:   &echo.DefaultBinder{},
	}
}

func (self *Binder) Bind(i any, c echo.Context) error {
	err := self.binder.Bind(i, c)
	if err != nil {
		return ErrBinderGeneric.Raise().Cause(err)
	}

	err = self.binder.BindHeaders(c, i)
	if err != nil {
		return ErrBinderGeneric.Raise().Cause(err)
	}

	return nil
}
