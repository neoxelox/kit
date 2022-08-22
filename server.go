package kit

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

var (
	_SERVER_DEFAULT_REQUEST_HEADER_MAX_SIZE     = 1 << 10 // 1 KB
	_SERVER_DEFAULT_REQUEST_BODY_MAX_SIZE       = 4 << 10 // 4 KB
	_SERVER_DEFAULT_REQUEST_FILE_MAX_SIZE       = 2 << 20 // 2 MB
	_SERVER_DEFAULT_REQUEST_KEEP_ALIVE_TIMEOUT  = 30 * time.Second
	_SERVER_DEFAULT_REQUEST_READ_TIMEOUT        = 30 * time.Second
	_SERVER_DEFAULT_REQUEST_READ_HEADER_TIMEOUT = 30 * time.Second
	_SERVER_DEFAULT_RESPONSE_WRITE_TIMEOUT      = 30 * time.Second
)

type ServerConfig struct {
	Environment              _environment
	AppPort                  int
	RequestHeaderMaxSize     *int
	RequestBodyMaxSize       *int
	RequestFileMaxSize       *int
	RequestKeepAliveTimeout  *time.Duration
	RequestReadTimeout       *time.Duration
	RequestReadHeaderTimeout *time.Duration
	ResponseWriteTimeout     *time.Duration
}

type Server struct {
	config   ServerConfig
	observer Observer
	server   *echo.Echo
}

func NewServer(observer Observer, serializer Serializer, binder Binder,
	renderer Renderer, exceptionHandler ExceptionHandler, config ServerConfig) *Server {
	if config.RequestHeaderMaxSize == nil {
		config.RequestHeaderMaxSize = ptr(_SERVER_DEFAULT_REQUEST_HEADER_MAX_SIZE)
	}

	if config.RequestBodyMaxSize == nil {
		config.RequestBodyMaxSize = ptr(_SERVER_DEFAULT_REQUEST_BODY_MAX_SIZE)
	}

	if config.RequestFileMaxSize == nil {
		config.RequestFileMaxSize = ptr(_SERVER_DEFAULT_REQUEST_FILE_MAX_SIZE)
	}

	if config.RequestKeepAliveTimeout == nil {
		config.RequestKeepAliveTimeout = ptr(_SERVER_DEFAULT_REQUEST_KEEP_ALIVE_TIMEOUT)
	}

	if config.RequestReadTimeout == nil {
		config.RequestReadTimeout = ptr(_SERVER_DEFAULT_REQUEST_READ_TIMEOUT)
	}

	if config.RequestReadHeaderTimeout == nil {
		config.RequestReadHeaderTimeout = ptr(_SERVER_DEFAULT_REQUEST_READ_HEADER_TIMEOUT)
	}

	if config.ResponseWriteTimeout == nil {
		config.ResponseWriteTimeout = ptr(_SERVER_DEFAULT_RESPONSE_WRITE_TIMEOUT)
	}

	server := echo.New()

	server.HideBanner = true
	server.HidePort = true
	server.DisableHTTP2 = true
	server.Debug = config.Environment == EnvDevelopment
	server.Server.MaxHeaderBytes = *config.RequestHeaderMaxSize
	server.Server.IdleTimeout = *config.RequestKeepAliveTimeout
	server.Server.ReadHeaderTimeout = *config.RequestReadHeaderTimeout
	server.Server.ReadTimeout = *config.RequestReadTimeout
	server.Server.WriteTimeout = *config.ResponseWriteTimeout

	// server.Logger = nil    // Observer should always be used instead
	// server.StdLogger = nil // Observer should always be used instead
	server.JSONSerializer = &serializer
	server.Binder = &binder
	server.Renderer = &renderer
	// server.Validator = nil // Validator should always be at domain level
	server.HTTPErrorHandler = exceptionHandler.Handle
	server.IPExtractor = echo.ExtractIPFromRealIPHeader() // TODO: maybe allow to modify this?

	// TODO: move this to a middleware in order to be able to log giant requests?
	server.Pre(echoMiddleware.BodyLimitWithConfig(echoMiddleware.BodyLimitConfig{
		Skipper: func(ctx echo.Context) bool {
			// TODO: allow to customize this or provide a built-in system for files?
			return strings.HasPrefix(ctx.Request().RequestURI, "/file")
		},
		Limit: Utils.ByteSize(*config.RequestBodyMaxSize),
	}))
	server.Pre(echoMiddleware.BodyLimitWithConfig(echoMiddleware.BodyLimitConfig{
		Limit: Utils.ByteSize(*config.RequestFileMaxSize),
	}))

	// Pre hook middleware
	server.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			ctx.Request().RemoteAddr = ctx.RealIP()
			return next(ctx)
		}
	})

	return &Server{
		config:   config,
		observer: observer,
		server:   server,
	}
}

func (self *Server) Run() error {
	self.observer.Infof("Server started at port %d", self.config.AppPort)

	err := self.server.Start(fmt.Sprintf(":%d", self.config.AppPort))
	if err != nil && err != http.ErrServerClosed {
		return ErrServerGeneric().Wrap(err)
	}

	return nil
}

func (self *Server) Use(middleware ...echo.MiddlewareFunc) {
	self.server.Pre(middleware...)
}

func (self *Server) Host(host string, middleware ...echo.MiddlewareFunc) *echo.Group {
	return self.server.Host(host, middleware...)
}

func (self *Server) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info("Closing server")

		self.server.Server.SetKeepAlivesEnabled(false)

		err := self.server.Shutdown(ctx)
		if err != nil {
			return ErrServerGeneric().WrapAs(err)
		}

		self.observer.Info("Closed server")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrServerTimedOut()
	default:
		return ErrServerGeneric().Wrap(err)
	}
}
