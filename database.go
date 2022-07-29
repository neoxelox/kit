package kit

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/leporo/sqlf"
	"github.com/randallmlough/pgxscan"
	"github.com/rs/zerolog"
)

const (
	_DATABASE_POSTGRES_DSN        = "postgresql://%s:%s@%s:%d/%s?sslmode=%s"
	_DATABASE_TRANSACTION_CTX_KEY = _BASE_CTX_KEY + "database:transaction"
)

var (
	_DATABASE_DEFAULT_MIN_CONNS          = 1
	_DATABASE_DEFAULT_MAX_CONNS          = 1 * runtime.GOMAXPROCS(-1)
	_DATABASE_DEFAULT_MAX_CONN_IDLE_TIME = 30 * time.Minute
	_DATABASE_DEFAULT_MAX_CONN_LIFE_TIME = 1 * time.Hour
	// _DATABASEDEFAULT_DIAL_TIMEOUT  = 30 * time.Second // TODO: check where this goes
	// _DATABASE_DEFAULT_ACQUIRE_TIMEOUT  = 30 * time.Second // TODO: check where this goes
	_DATABASE_DEFAULT_RETRY_ATTEMPTS      = 1
	_DATABASE_DEFAULT_RETRY_INITIAL_DELAY = 0 * time.Second
	_DATABASE_DEFAULT_RETRY_LIMIT_DELAY   = 0 * time.Second
	_DATABASE_ERR_PGCODE                  = regexp.MustCompile(`\(SQLSTATE (.*)\)`)
)

var _KlevelToPlevel = map[_level]pgx.LogLevel{
	LvlTrace: pgx.LogLevelTrace,
	LvlDebug: pgx.LogLevelDebug,
	LvlInfo:  pgx.LogLevelInfo,
	LvlWarn:  pgx.LogLevelWarn,
	LvlError: pgx.LogLevelError,
	LvlNone:  pgx.LogLevelNone,
}

type DatabaseRetryConfig struct {
	Attempts     int
	InitialDelay time.Duration
	LimitDelay   time.Duration
}

type DatabaseConfig struct {
	DatabaseHost            string
	DatabasePort            int
	DatabaseSSLMode         string
	DatabaseUser            string
	DatabasePassword        string
	DatabaseName            string
	AppName                 string
	DatabaseMinConns        *int
	DatabaseMaxConns        *int
	DatabaseMaxConnIdleTime *time.Duration
	DatabaseMaxConnLifeTime *time.Duration
	RetryConfig             *DatabaseRetryConfig
}

type Database struct {
	config   DatabaseConfig
	observer Observer
	pool     *pgxpool.Pool
}

func NewDatabase(ctx context.Context, observer Observer, config DatabaseConfig) (*Database, error) {
	observer.Anchor()

	if config.DatabaseMinConns == nil {
		config.DatabaseMinConns = ptr(_DATABASE_DEFAULT_MIN_CONNS)
	}

	if config.DatabaseMaxConns == nil {
		config.DatabaseMaxConns = ptr(_DATABASE_DEFAULT_MAX_CONNS)
	}

	if config.DatabaseMaxConnIdleTime == nil {
		config.DatabaseMaxConnIdleTime = ptr(_DATABASE_DEFAULT_MAX_CONN_IDLE_TIME)
	}

	if config.DatabaseMaxConnLifeTime == nil {
		config.DatabaseMaxConnLifeTime = ptr(_DATABASE_DEFAULT_MAX_CONN_LIFE_TIME)
	}

	if config.RetryConfig == nil {
		config.RetryConfig = &DatabaseRetryConfig{
			Attempts:     _DATABASE_DEFAULT_RETRY_ATTEMPTS,
			InitialDelay: _DATABASE_DEFAULT_RETRY_INITIAL_DELAY,
			LimitDelay:   _DATABASE_DEFAULT_RETRY_LIMIT_DELAY,
		}
	}

	dsn := fmt.Sprintf(
		_DATABASE_POSTGRES_DSN,
		config.DatabaseUser,
		config.DatabasePassword,
		config.DatabaseHost,
		config.DatabasePort,
		config.DatabaseName,
		config.DatabaseSSLMode,
	)

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, ErrDatabaseGeneric().Wrap(err)
	}

	poolConfig.MinConns = int32(*config.DatabaseMinConns)
	poolConfig.MaxConns = int32(*config.DatabaseMaxConns)
	poolConfig.MaxConnIdleTime = *config.DatabaseMaxConnIdleTime
	poolConfig.MaxConnLifetime = *config.DatabaseMaxConnLifeTime
	poolConfig.ConnConfig.RuntimeParams["standard_conforming_strings"] = "on"
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.AppName

	pgxLogger := _newPgxLogger(observer.Logger)
	pgxLogLevel := _KlevelToPlevel[pgxLogger.logger.Level()]

	// PGX Info level is too much! (PGX levels are reversed)
	if pgxLogLevel >= pgx.LogLevelInfo {
		pgxLogLevel = pgx.LogLevelError
	}

	poolConfig.ConnConfig.Logger = pgxLogger
	poolConfig.ConnConfig.LogLevel = pgxLogLevel

	var pool *pgxpool.Pool

	// TODO: only retry on specific errors
	err = Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		return Utils.ExponentialRetry(
			config.RetryConfig.Attempts, config.RetryConfig.InitialDelay, config.RetryConfig.LimitDelay,
			nil, func(attempt int) error {
				var err error // nolint

				observer.Infof("Trying to connect to the %s database %d/%d",
					config.DatabaseName, attempt, config.RetryConfig.Attempts)

				pool, err = pgxpool.ConnectConfig(ctx, poolConfig)
				if err != nil {
					return ErrDatabaseGeneric().WrapAs(err)
				}

				err = pool.Ping(ctx)
				if err != nil {
					return ErrDatabaseGeneric().WrapAs(err)
				}

				return nil
			})
	})
	switch {
	case err == nil:
	case ErrDeadlineExceeded().Is(err):
		return nil, ErrDatabaseTimedOut()
	default:
		return nil, ErrDatabaseGeneric().Wrap(err)
	}

	observer.Infof("Connected to the %s database", config.DatabaseName)

	sqlf.SetDialect(sqlf.PostgreSQL)

	return &Database{
		observer: observer,
		config:   config,
		pool:     pool,
	}, nil
}

func (self *Database) Health(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		currentConns := self.pool.Stat().TotalConns()
		if currentConns < int32(*self.config.DatabaseMinConns) {
			return ErrDatabaseUnhealthy().Withf("current conns %d below minimum %d",
				currentConns, *self.config.DatabaseMinConns)
		}

		err := self.pool.Ping(ctx)
		if err != nil {
			return ErrDatabaseUnhealthy().WrapAs(err)
		}

		err = ctx.Err()
		if err != nil {
			return ErrDatabaseUnhealthy().WrapAs(err)
		}

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrDatabaseTimedOut()
	default:
		return ErrDatabaseGeneric().Wrap(err)
	}
}

func _dbErrToError(err error) *Error {
	if err == nil {
		return nil
	}

	if code := _DATABASE_ERR_PGCODE.FindStringSubmatch(err.Error()); len(code) == 2 {
		switch code[1] {
		case pgerrcode.IntegrityConstraintViolation, pgerrcode.RestrictViolation, pgerrcode.NotNullViolation,
			pgerrcode.ForeignKeyViolation, pgerrcode.UniqueViolation, pgerrcode.CheckViolation,
			pgerrcode.ExclusionViolation:
			return ErrDatabaseIntegrityViolation().WrapWithDepth(1, err)
		}
	}

	switch err.Error() {
	case pgx.ErrNoRows.Error():
		return ErrDatabaseNoRows().WrapWithDepth(1, err)
	default:
		return ErrDatabaseGeneric().WrapWithDepth(1, err)
	}
}

func (self *Database) Query(ctx context.Context, stmt *sqlf.Stmt) error {
	defer stmt.Close()

	var rows pgx.Rows
	var err error

	if ctx.Value(_DATABASE_TRANSACTION_CTX_KEY) != nil {
		rows, err = ctx.Value(_DATABASE_TRANSACTION_CTX_KEY).(pgx.Tx).Query(ctx, stmt.String(), stmt.Args()...)
	} else {
		rows, err = self.pool.Query(ctx, stmt.String(), stmt.Args()...)
	}

	if err != nil {
		return _dbErrToError(err)
	}

	err = ctx.Err()
	if err != nil {
		return _dbErrToError(err)
	}

	err = pgxscan.NewScanner(rows).Scan(stmt.Dest()...)
	if err != nil {
		return _dbErrToError(err)
	}

	return nil
}

func (self *Database) Exec(ctx context.Context, stmt *sqlf.Stmt) (int, error) {
	defer stmt.Close()

	var command pgconn.CommandTag
	var err error

	if ctx.Value(_DATABASE_TRANSACTION_CTX_KEY) != nil {
		command, err = ctx.Value(_DATABASE_TRANSACTION_CTX_KEY).(pgx.Tx).Exec(ctx, stmt.String(), stmt.Args()...)
	} else {
		command, err = self.pool.Exec(ctx, stmt.String(), stmt.Args()...)
	}

	if err != nil {
		return 0, _dbErrToError(err)
	}

	err = ctx.Err()
	if err != nil {
		return 0, _dbErrToError(err)
	}

	return int(command.RowsAffected()), nil
}

func (self *Database) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(_DATABASE_TRANSACTION_CTX_KEY) != nil {
		err := fn(ctx)
		if err != nil {
			return ErrDatabaseTransactionFailed().WrapAs(err)
		}

		return nil
	}

	transaction, err := self.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return ErrDatabaseTransactionFailed().Wrap(err)
	}

	err = ctx.Err()
	if err != nil {
		return ErrDatabaseTransactionFailed().Wrap(err)
	}

	defer func() {
		err := recover()
		if err != nil {
			errR := transaction.Rollback(ctx)
			panic(Utils.CombineErrors(err.(error), errR)) // nolint
		}
	}()

	err = fn(context.WithValue(ctx, _DATABASE_TRANSACTION_CTX_KEY, transaction))
	if err != nil {
		errR := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed().Wrap(Utils.CombineErrors(err, errR))
	}

	err = ctx.Err()
	if err != nil {
		errR := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed().Wrap(Utils.CombineErrors(err, errR))
	}

	err = transaction.Commit(ctx)
	if err != nil {
		errR := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed().Wrap(Utils.CombineErrors(err, errR))
	}

	err = ctx.Err()
	if err != nil {
		errR := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed().Wrap(Utils.CombineErrors(err, errR))
	}

	return nil
}

func (self *Database) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Infof("Closing %s database", self.config.DatabaseName)

		self.pool.Close()

		self.observer.Infof("Closed %s database", self.config.DatabaseName)

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrDatabaseTimedOut()
	default:
		return ErrDatabaseGeneric().Wrap(err)
	}
}

var _PlevelToZlevel = map[pgx.LogLevel]zerolog.Level{
	pgx.LogLevelTrace: zerolog.TraceLevel,
	pgx.LogLevelDebug: zerolog.DebugLevel,
	pgx.LogLevelInfo:  zerolog.InfoLevel,
	pgx.LogLevelWarn:  zerolog.WarnLevel,
	pgx.LogLevelError: zerolog.ErrorLevel,
	pgx.LogLevelNone:  zerolog.Disabled,
}

type _pgxLogger struct {
	logger Logger
}

func _newPgxLogger(logger Logger) *_pgxLogger {
	return &_pgxLogger{
		logger: logger,
	}
}

func (self _pgxLogger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]interface{}) { // nolint
	self.logger.Logger().WithLevel(_PlevelToZlevel[level]).Fields(data).Msg(msg)
}
