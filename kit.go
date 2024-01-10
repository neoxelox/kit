// Package kit implements a highly opinionated Go backend kit.
package kit

import (
	"net/http"
)

type Key string

// Builtin context/cache keys.
var (
	KeyBase                Key = "kit:"
	KeyDatabaseTransaction Key = KeyBase + "database:transaction"
	KeyLocalizerLocale     Key = KeyBase + "localizer:locale"
	KeyTraceID             Key = KeyBase + "trace:id"
)

type Environment string

// Builtin environments.
var (
	EnvDevelopment Environment = "dev"
	EnvIntegration Environment = "ci"
	EnvProduction  Environment = "prod"
)

type Level int

// Builtin levels.
var (
	LvlTrace Level = -5
	LvlDebug Level = -4
	LvlInfo  Level = -3
	LvlWarn  Level = -2
	LvlError Level = -1
	LvlNone  Level
)

// Builtin errors.
var (
	ErrLoggerGeneric              = NewError("logger failed")
	ErrLoggerTimedOut             = NewError("logger timed out")
	ErrBinderGeneric              = NewError("binder failed")
	ErrExceptionHandlerGeneric    = NewError("error handler failed")
	ErrMigratorGeneric            = NewError("migrator failed")
	ErrMigratorTimedOut           = NewError("migrator timed out")
	ErrObserverGeneric            = NewError("observer failed")
	ErrObserverTimedOut           = NewError("observer timed out")
	ErrSerializerGeneric          = NewError("serializer failed")
	ErrRendererGeneric            = NewError("renderer failed")
	ErrLocalizerGeneric           = NewError("localizer failed")
	ErrServerGeneric              = NewError("server failed")
	ErrServerTimedOut             = NewError("server timed out")
	ErrDatabaseGeneric            = NewError("database failed")
	ErrDatabaseTimedOut           = NewError("database timed out")
	ErrDatabaseUnhealthy          = NewError("database unhealthy")
	ErrDatabaseTransactionFailed  = NewError("database transaction failed")
	ErrDatabaseNoRows             = NewError("database no rows in result set")
	ErrDatabaseIntegrityViolation = NewError("database integrity constraint violation")
	ErrCacheGeneric               = NewError("cache failed")
	ErrCacheTimedOut              = NewError("cache timed out")
	ErrCacheUnhealthy             = NewError("cache unhealthy")
	ErrCacheMiss                  = NewError("cache key not found")
	ErrWorkerGeneric              = NewError("worker failed")
	ErrWorkerTimedOut             = NewError("worker timed out")
	ErrEnqueuerGeneric            = NewError("enqueuer failed")
	ErrEnqueuerTimedOut           = NewError("enqueuer timed out")
)

// Builtin exceptions.
var (
	ExcServerGeneric     = NewException(http.StatusInternalServerError, "ERR_SERVER_GENERIC")
	ExcServerUnavailable = NewException(http.StatusServiceUnavailable, "ERR_SERVER_UNAVAILABLE")
	ExcRequestTimeout    = NewException(http.StatusGatewayTimeout, "ERR_REQUEST_TIMEOUT")
	ExcClientGeneric     = NewException(http.StatusBadRequest, "ERR_CLIENT_GENERIC")
	ExcInvalidRequest    = NewException(http.StatusBadRequest, "ERR_INVALID_REQUEST")
	ExcNotFound          = NewException(http.StatusNotFound, "ERR_NOT_FOUND")
	ExcUnauthorized      = NewException(http.StatusUnauthorized, "ERR_UNAUTHORIZED")
)
