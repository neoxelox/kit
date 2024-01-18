package middleware

import (
	"context"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
	"github.com/neoxelox/kit/util"
)

// TODO: check whether to merge the recover middleware with the observer one as it is not protected
// because the observer middleware has to be the first one in order to log the responses of the panicks

var (
	_RECOVER_MIDDLEWARE_DEFAULT_CONFIG = RecoverConfig{}
)

type RecoverConfig struct {
}

type Recover struct {
	config   RecoverConfig
	observer *kit.Observer
}

func NewRecover(observer *kit.Observer, config RecoverConfig) *Recover {
	util.Merge(&config, _RECOVER_MIDDLEWARE_DEFAULT_CONFIG)

	return &Recover{
		config:   config,
		observer: observer,
	}
}

func (self *Recover) HandleRequest(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		defer func() {
			rec := recover()
			if rec != nil {
				err, ok := rec.(error)
				if !ok {
					err = kit.ErrHTTPServerGeneric.Raise().With("%v", rec)
				}

				if err == http.ErrAbortHandler {
					panic(err)
				}

				// Pass error to the error handler to serialize and write error response
				ctx.Error(err)
			}
		}()

		return next(ctx)
	}
}

func (self *Recover) HandleTask(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		defer func() {
			rec := recover()
			if rec != nil {
				err, ok := rec.(error)
				if !ok {
					err = kit.ErrWorkerGeneric.Raise().With("%v", rec)
				}

				// Log error ourselves ...?
				// TODO: Test if the worker survives with a panic
				// and check whether the error is passed to the observer???
				self.observer.Error(ctx, err)
			}
		}()

		return next.ProcessTask(ctx, task)
	})
}
