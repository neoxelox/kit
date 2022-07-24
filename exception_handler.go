package kit

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type ExceptionHandlerConfig struct {
	Environment string
}

type ExceptionHandler struct {
	config ExceptionHandlerConfig
	logger Logger
}

func NewExceptionHandler(logger Logger, config ExceptionHandlerConfig) *ExceptionHandler {
	logger.SetFile()

	return &ExceptionHandler{
		logger: logger,
		config: config,
	}
}

func (self *ExceptionHandler) Handle(err error, ctx echo.Context) {
	var exc *Exception

	exc, ok := err.(*Exception)
	if !ok {
		switch err {
		case echo.ErrNotFound:
			exc = Exceptions.ExcNotFound().Cause(err)
		case echo.ErrStatusRequestEntityTooLarge:
			exc = Exceptions.ExcInvalidRequest().Cause(err)
		case http.ErrHandlerTimeout:
			exc = Exceptions.ExcRequestTimeout().Cause(err)
		default: // Fallback.
			exc = Exceptions.ExcServerGeneric().Cause(err)
		}
	}

	if exc.status >= http.StatusInternalServerError {
		// TODO: send to Sentry and/or New Relic
		self.logger.Error(exc)
	}

	if ctx.Response().Committed {
		return
	}

	if ctx.Request().Method == http.MethodHead {
		err = ctx.NoContent(exc.status)
	} else {
		if self.config.Environment != Environments.Development {
			exc.Redact()
		}

		err = ctx.JSON(exc.status, exc)
	}

	if err != nil {
		self.logger.Error(Errors.ErrExceptionHandlerGeneric().Withf("cannot return exception %s", exc).Wrap(err))
	}
}
