package kit

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit/util"
)

var (
	_EXCEPTION_HANDLER_DEFAULT_CONFIG = ExceptionHandlerConfig{}
)

type ExceptionHandlerConfig struct {
	Environment Environment
}

type ExceptionHandler struct {
	config   ExceptionHandlerConfig
	observer Observer
}

func NewExceptionHandler(observer Observer, config ExceptionHandlerConfig) *ExceptionHandler {
	util.Merge(&config, _EXCEPTION_HANDLER_DEFAULT_CONFIG)

	return &ExceptionHandler{
		observer: observer,
		config:   config,
	}
}

func (self *ExceptionHandler) Handle(err error, ctx echo.Context) {
	// If response was already committed it means another middleware or an actual view
	// has already called the exception handler or written an appropriate response.
	if ctx.Response().Committed {
		return
	}

	var exc *Exception

	exc, ok := err.(*Exception)
	if !ok {
		switch err {
		case echo.ErrNotFound:
			exc = ExcNotFound().Cause(err)
		case echo.ErrMethodNotAllowed:
			exc = ExcInvalidRequest().Cause(err)
		case echo.ErrStatusRequestEntityTooLarge:
			exc = ExcInvalidRequest().Cause(err)
		case http.ErrHandlerTimeout:
			exc = ExcRequestTimeout().Cause(err)
		default: // Fallback.
			exc = ExcServerGeneric().Cause(err)
		}
	}

	if exc.status >= http.StatusInternalServerError {
		self.observer.Error(ctx.Request().Context(), exc)
	}

	if ctx.Request().Method == http.MethodHead {
		err = ctx.NoContent(exc.status)
	} else {
		if self.config.Environment != EnvDevelopment {
			exc.Redact()
		}

		err = ctx.JSON(exc.status, exc)
	}

	if err != nil {
		self.observer.Error(
			ctx.Request().Context(), ErrExceptionHandlerGeneric().Withf("cannot return exception %s", exc).Wrap(err))
	}
}
