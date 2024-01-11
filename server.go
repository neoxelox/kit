package kit

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"

	"github.com/neoxelox/kit/util"
)

var (
	_SERVER_DEFAULT_CONFIG = ServerConfig{
		RequestHeaderMaxSize:     util.Pointer(1 << 10), // 1 KB
		RequestBodyMaxSize:       util.Pointer(4 << 10), // 4 KB
		RequestFileMaxSize:       util.Pointer(2 << 20), // 2 MB
		RequestFilePattern:       util.Pointer(`.*/file.*`),
		RequestKeepAliveTimeout:  util.Pointer(30 * time.Second),
		RequestReadTimeout:       util.Pointer(30 * time.Second),
		RequestReadHeaderTimeout: util.Pointer(30 * time.Second),
		RequestIPExtractor:       util.Pointer((func(*http.Request) string)(echo.ExtractIPFromRealIPHeader())),
		ResponseWriteTimeout:     util.Pointer(30 * time.Second),
	}
)

type ServerConfig struct {
	Environment              Environment
	Port                     int
	RequestHeaderMaxSize     *int
	RequestBodyMaxSize       *int
	RequestFileMaxSize       *int
	RequestFilePattern       *string
	RequestKeepAliveTimeout  *time.Duration
	RequestReadTimeout       *time.Duration
	RequestReadHeaderTimeout *time.Duration
	RequestIPExtractor       *func(*http.Request) string
	ResponseWriteTimeout     *time.Duration
}

type Server struct {
	config   ServerConfig
	observer Observer
	server   *echo.Echo
}

func NewServer(observer Observer, serializer Serializer, binder Binder,
	renderer Renderer, exceptionHandler ExceptionHandler, config ServerConfig) *Server {
	util.Merge(&config, _SERVER_DEFAULT_CONFIG)

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
	server.IPExtractor = *config.RequestIPExtractor

	requestFilePattern := regexp.MustCompile(*config.RequestFilePattern)
	server.Pre(echoMiddleware.BodyLimitWithConfig(echoMiddleware.BodyLimitConfig{
		Skipper: func(ctx echo.Context) bool {
			return requestFilePattern.MatchString(ctx.Request().RequestURI)
		},
		Limit: util.ByteSize(*config.RequestBodyMaxSize),
	}))
	server.Pre(echoMiddleware.BodyLimitWithConfig(echoMiddleware.BodyLimitConfig{
		Limit: util.ByteSize(*config.RequestFileMaxSize),
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

func (self *Server) Run(ctx context.Context) error {
	self.observer.Infof(ctx, "Server started at port %d", self.config.Port)

	err := self.server.Start(fmt.Sprintf(":%d", self.config.Port))
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

func (self *Server) Default(middleware ...echo.MiddlewareFunc) *echo.Group {
	return self.server.Group("", middleware...)
}

func (self *Server) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info(ctx, "Closing server")

		self.server.Server.SetKeepAlivesEnabled(false)

		err := self.server.Shutdown(ctx)
		if err != nil {
			return ErrServerGeneric().WrapAs(err)
		}

		self.observer.Info(ctx, "Closed server")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case util.ErrDeadlineExceeded.Is(err):
		return ErrServerTimedOut()
	default:
		return ErrServerGeneric().Wrap(err)
	}
}
