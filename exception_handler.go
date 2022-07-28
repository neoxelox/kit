package kit

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type ExceptionHandlerConfig struct {
	Environment _environment
}

type ExceptionHandler struct {
	config   ExceptionHandlerConfig
	observer Observer
}

func NewExceptionHandler(observer Observer, config ExceptionHandlerConfig) *ExceptionHandler {
	observer.Anchor()

	return &ExceptionHandler{
		observer: observer,
		config:   config,
	}
}

func (self *ExceptionHandler) Handle(err error, ctx echo.Context) {
	var exc *Exception

	exc, ok := err.(*Exception)
	if !ok {
		switch err {
		case echo.ErrNotFound:
			exc = ExcNotFound().Cause(err)
		case echo.ErrStatusRequestEntityTooLarge:
			exc = ExcInvalidRequest().Cause(err)
		case http.ErrHandlerTimeout:
			exc = ExcRequestTimeout().Cause(err)
		default: // Fallback.
			exc = ExcServerGeneric().Cause(err)
		}
	}

	if exc.status >= http.StatusInternalServerError {
		self.observer.Error(exc)
	}

	if ctx.Response().Committed {
		return
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
		self.observer.Error(ErrExceptionHandlerGeneric().Withf("cannot return exception %s", exc).Wrap(err))
	}
}
