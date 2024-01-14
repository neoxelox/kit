package kit

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

var (
	ErrHTTPErrorHandlerGeneric = errors.New("http error handler failed")
)

var (
	_HTTP_ERROR_HANDLER_DEFAULT_CONFIG = HTTPErrorHandlerConfig{
		MinStatusCodeToLog: util.Pointer(http.StatusInternalServerError),
	}
)

type HTTPErrorHandlerConfig struct {
	Environment        Environment
	MinStatusCodeToLog *int
}

type HTTPErrorHandler struct {
	config   HTTPErrorHandlerConfig
	observer *Observer
}

func NewHTTPErrorHandler(observer *Observer, config HTTPErrorHandlerConfig) *HTTPErrorHandler {
	util.Merge(&config, _HTTP_ERROR_HANDLER_DEFAULT_CONFIG)

	return &HTTPErrorHandler{
		observer: observer,
		config:   config,
	}
}

func (self *HTTPErrorHandler) Handle(err error, ctx echo.Context) {
	// If response was already committed it means another middleware or an actual view
	// has already called the error handler or has written an appropriate response.
	if ctx.Response().Committed || err == nil {
		return
	}

	httpError := HTTPErrServerGeneric.Cause(err)

	httpError, ok := err.(*HTTPError)
	if !ok {
		*httpError, ok = err.(HTTPError)

		if !ok {
			switch err {
			case echo.ErrNotFound:
				httpError = HTTPErrNotFound.Cause(err)
			case echo.ErrMethodNotAllowed:
				httpError = HTTPErrInvalidRequest.Cause(err)
			case echo.ErrStatusRequestEntityTooLarge:
				httpError = HTTPErrInvalidRequest.Cause(err)
			case http.ErrHandlerTimeout:
				httpError = HTTPErrRequestTimeout.Cause(err)
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
			ErrHTTPErrorHandlerGeneric.Raise().
				With("cannot respond http error %s", httpError.Code()).
				Extra(map[string]any{"http_error": httpError}).
				Cause(err))
	}
}
