package middleware

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/scylladb/go-set/strset"

	"github.com/neoxelox/kit"
	"github.com/neoxelox/kit/util"
)

// TODO: add CSRF Middleware

var (
	_SECURE_MIDDLEWARE_DEFAULT_CORS_ALLOW_ORIGINS      = []string{"*"}
	_SECURE_MIDDLEWARE_DEFAULT_CORS_ALLOW_METHODS      = []string{"*"}
	_SECURE_MIDDLEWARE_DEFAULT_CORS_ALLOW_HEADERS      = []string{"*"}
	_SECURE_MIDDLEWARE_DEFAULT_CORS_MAX_AGE            = 86400
	_SECURE_MIDDLEWARE_DEFAULT_XSS_PROTECTION          = "1; mode=block"
	_SECURE_MIDDLEWARE_DEFAULT_X_FRAME_OPTIONS         = "SAMEORIGIN"
	_SECURE_MIDDLEWARE_DEFAULT_HSTS_EXCLUDE_SUBDOMAINS = false
	_SECURE_MIDDLEWARE_DEFAULT_HSTS_PRELOAD_ENABLED    = true
	_SECURE_MIDDLEWARE_DEFAULT_HSTS_MAX_AGE            = 31536000
	_SECURE_MIDDLEWARE_DEFAULT_CONTENT_TYPE_NOSNIFF    = "nosniff"
	_SECURE_MIDDLEWARE_DEFAULT_CONTENT_SECURITY_POLICY = "default-src"
	_SECURE_MIDDLEWARE_DEFAULT_CSP_REPORT_ONLY         = false
	_SECURE_MIDDLEWARE_DEFAULT_REFERRER_POLICY         = "same-origin"
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
	observer         kit.Observer
	corsMiddleware   echo.MiddlewareFunc
	secureMiddleware echo.MiddlewareFunc
}

func NewSecure(observer kit.Observer, config SecureConfig) *Secure {
	if config.CORSAllowOrigins == nil {
		config.CORSAllowOrigins = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CORS_ALLOW_ORIGINS)
	}

	*config.CORSAllowOrigins = strset.New(*config.CORSAllowOrigins...).List()

	if config.CORSAllowMethods == nil {
		config.CORSAllowMethods = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CORS_ALLOW_METHODS)
	}

	if config.CORSAllowHeaders == nil {
		config.CORSAllowHeaders = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CORS_ALLOW_HEADERS)
	}

	if config.CORSMaxAge == nil {
		config.CORSMaxAge = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CORS_MAX_AGE)
	}

	if config.XSSProtection == nil {
		config.XSSProtection = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_XSS_PROTECTION)
	}

	if config.XFrameOptions == nil {
		config.XFrameOptions = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_X_FRAME_OPTIONS)
	}

	if config.HSTSExcludeSubdomains == nil {
		config.HSTSExcludeSubdomains = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_HSTS_EXCLUDE_SUBDOMAINS)
	}

	if config.HSTSPreloadEnabled == nil {
		config.HSTSPreloadEnabled = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_HSTS_PRELOAD_ENABLED)
	}

	if config.HSTSMaxAge == nil {
		config.HSTSMaxAge = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_HSTS_MAX_AGE)
	}

	if config.ContentTypeNosniff == nil {
		config.ContentTypeNosniff = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CONTENT_TYPE_NOSNIFF)
	}

	if config.ContentSecurityPolicy == nil {
		config.ContentSecurityPolicy = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CONTENT_SECURITY_POLICY)
	}

	*config.ContentSecurityPolicy = fmt.Sprintf(
		"%s %s", *config.ContentSecurityPolicy, strings.Join(*config.CORSAllowOrigins, " "))

	if config.CSPReportOnly == nil {
		config.CSPReportOnly = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_CSP_REPORT_ONLY)
	}

	if config.ReferrerPolicy == nil {
		config.ReferrerPolicy = util.Pointer(_SECURE_MIDDLEWARE_DEFAULT_REFERRER_POLICY)
	}

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
