package middleware

import (
	"fmt"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/scylladb/go-set/strset"

	"github.com/neoxelox/kit"
	"github.com/neoxelox/kit/util"
)

// TODO: add CSRF Middleware

var (
	_SECURE_MIDDLEWARE_DEFAULT_CONFIG = SecureConfig{
		CORSAllowOrigins:      util.Pointer([]string{"*"}),
		CORSAllowMethods:      util.Pointer([]string{"*"}),
		CORSAllowHeaders:      util.Pointer([]string{"*"}),
		CORSMaxAge:            util.Pointer(int((24 * time.Hour).Seconds())),
		XSSProtection:         util.Pointer("1; mode=block"),
		XFrameOptions:         util.Pointer("SAMEORIGIN"),
		HSTSExcludeSubdomains: util.Pointer(false),
		HSTSPreloadEnabled:    util.Pointer(true),
		HSTSMaxAge:            util.Pointer(int((365 * 24 * time.Hour).Seconds())),
		ContentTypeNosniff:    util.Pointer("nosniff"),
		ContentSecurityPolicy: util.Pointer("default-src"),
		CSPReportOnly:         util.Pointer(false),
		ReferrerPolicy:        util.Pointer("same-origin"),
	}
)

type SecureConfig struct {
	CORSAllowOrigins      *[]string
	CORSAllowMethods      *[]string
	CORSAllowHeaders      *[]string
	CORSMaxAge            *int
	XSSProtection         *string
	XFrameOptions         *string
	HSTSExcludeSubdomains *bool
	HSTSPreloadEnabled    *bool
	HSTSMaxAge            *int
	ContentTypeNosniff    *string
	ContentSecurityPolicy *string
	CSPReportOnly         *bool
	ReferrerPolicy        *string
}

type Secure struct {
	config           SecureConfig
	observer         *kit.Observer
	corsMiddleware   echo.MiddlewareFunc
	secureMiddleware echo.MiddlewareFunc
}

func NewSecure(observer *kit.Observer, config SecureConfig) *Secure {
	util.Merge(&config, _SECURE_MIDDLEWARE_DEFAULT_CONFIG)

	*config.CORSAllowOrigins = strset.New(*config.CORSAllowOrigins...).List()
	*config.ContentSecurityPolicy = fmt.Sprintf(
		"%s %s", *config.ContentSecurityPolicy, strings.Join(*config.CORSAllowOrigins, " "))

	corsMiddleware := echoMiddleware.CORSWithConfig(echoMiddleware.CORSConfig{
		AllowOrigins: *config.CORSAllowOrigins,
		AllowMethods: *config.CORSAllowMethods,
		AllowHeaders: *config.CORSAllowHeaders,
		MaxAge:       *config.CORSMaxAge,
	})

	secureMiddleware := echoMiddleware.SecureWithConfig(echoMiddleware.SecureConfig{
		XSSProtection:         *config.XSSProtection,
		XFrameOptions:         *config.XFrameOptions,
		HSTSExcludeSubdomains: *config.HSTSExcludeSubdomains,
		HSTSPreloadEnabled:    *config.HSTSPreloadEnabled,
		HSTSMaxAge:            *config.HSTSMaxAge,
		ContentTypeNosniff:    *config.ContentTypeNosniff,
		ContentSecurityPolicy: *config.ContentSecurityPolicy,
		CSPReportOnly:         *config.CSPReportOnly,
		ReferrerPolicy:        *config.ReferrerPolicy,
	})

	return &Secure{
		config:           config,
		observer:         observer,
		corsMiddleware:   corsMiddleware,
		secureMiddleware: secureMiddleware,
	}
}

func (self *Secure) Handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		return self.secureMiddleware(self.corsMiddleware(next))(ctx)
	}
}
