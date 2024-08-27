package kit

import (
	"context"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/mkideal/cli"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

var (
	ErrErrorHandlerGeneric = errors.New("error handler failed")
)

var (
	_ERROR_HANDLER_DEFAULT_CONFIG = ErrorHandlerConfig{
		MinStatusCodeToLog: util.Pointer(http.StatusInternalServerError),
	}
)

type ErrorHandlerConfig struct {
	Environment        Environment
	MinStatusCodeToLog *int
}

type ErrorHandler struct {
	config   ErrorHandlerConfig
	observer *Observer
}

func NewErrorHandler(observer *Observer, config ErrorHandlerConfig) *ErrorHandler {
	util.Merge(&config, _ERROR_HANDLER_DEFAULT_CONFIG)

	return &ErrorHandler{
		observer: observer,
		config:   config,
	}
}

func (self *ErrorHandler) HandleRequest(err error, ctx echo.Context) {
	// If response was already committed it means another middleware or an actual view
	// has already called the error handler or has written an appropriate response.
	if ctx.Response().Committed || err == nil {
		return
	}

	httpError, ok := err.(*HTTPError)
	if !ok {
		httpErrorV, ok := err.(HTTPError) // nolint:govet
		httpError = &httpErrorV

		if !ok {
			switch err {
			case echo.ErrNotFound:
				httpError = HTTPErrNotFound.Cause(err)
			case echo.ErrMethodNotAllowed:
				httpError = HTTPErrInvalidRequest.Cause(err)
			case echo.ErrStatusRequestEntityTooLarge:
				httpError = HTTPErrInvalidRequest.Cause(err)
			case http.ErrHandlerTimeout:
				httpError = HTTPErrServerTimeout.Cause(err)
			default:
				httpError = HTTPErrServerGeneric.Cause(err)
			}
		}
	}

	if httpError.Status() >= *self.config.MinStatusCodeToLog {
		self.observer.Error(ctx.Request().Context(), httpError)
	}

	if ctx.Request().Method == http.MethodHead {
		err = ctx.NoContent(httpError.Status())
	} else {
		if self.config.Environment != EnvDevelopment {
			httpError.Redact()
		}

		err = ctx.JSON(httpError.Status(), httpError)
	}

	if err != nil {
		self.observer.Error(ctx.Request().Context(),
			ErrErrorHandlerGeneric.Raise().
				With("cannot respond http error %s", httpError.Code()).
				Extra(map[string]any{"http_error": httpError}).
				Cause(err))
	}
}

func (self *ErrorHandler) HandleTask(ctx context.Context, _ *asynq.Task, err error) {
	if err == nil {
		return
	}

	self.observer.Error(ctx, err)
}

func (self *ErrorHandler) HandleCommand(next RunnerHandler) RunnerHandler {
	return func(ctx context.Context, command *cli.Context) error {
		err := next(ctx, command)
		if err != nil && err != cli.ExitError {
			self.observer.Error(ctx, err)

			return err
		}

		return nil
	}
}
