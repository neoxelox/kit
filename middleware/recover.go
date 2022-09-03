package middleware

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

// TODO: check whether to merge the recover middleware with the observer one as it is not protected

type RecoverConfig struct {
}

type Recover struct {
	config   RecoverConfig
	observer kit.Observer
}

func NewRecover(observer kit.Observer, config RecoverConfig) *Recover {
	return &Recover{
		config:   config,
		observer: observer,
	}
}

func (self *Recover) Handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		defer func() {
			rec := recover()
			if rec != nil {
				err, ok := rec.(error)
				if !ok {
					err = kit.ErrServerGeneric().With(fmt.Sprint(rec))
				}

				if err == http.ErrAbortHandler {
					panic(err)
				}

				// Handle, serialize and write panic exception response
				ctx.Error(err)
			}
		}()

		return next(ctx)
	}
}
