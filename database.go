package kit

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/leporo/sqlf"
	"github.com/neoxelox/kit/util"
	"github.com/randallmlough/pgxscan"
)

const (
	_DATABASE_POSTGRES_DSN = "postgresql://%s:%s@%s:%d/%s?sslmode=%s"
)

var (
	_DATABASE_DEFAULT_MIN_CONNS               = 1
	_DATABASE_DEFAULT_MAX_CONNS               = 1 * runtime.GOMAXPROCS(-1)
	_DATABASE_DEFAULT_MAX_CONN_IDLE_TIME      = 30 * time.Minute
	_DATABASE_DEFAULT_MAX_CONN_LIFE_TIME      = 1 * time.Hour
	_DATABASE_DEFAULT_DIAL_TIMEOUT            = 30 * time.Second
	_DATABASE_DEFAULT_STATEMENT_TIMEOUT       = 30 * time.Second
	_DATABASE_DEFAULT_LOCK_TIMEOUT            = 30 * time.Second
	_DATABASE_DEFAULT_DEFAULT_ISOLATION_LEVEL = IsoLvlReadCommitted
	_DATABASE_DEFAULT_RETRY_ATTEMPTS          = 1
	_DATABASE_DEFAULT_RETRY_INITIAL_DELAY     = 0 * time.Second
	_DATABASE_DEFAULT_RETRY_LIMIT_DELAY       = 0 * time.Second
	_DATABASE_ERR_PGCODE                      = regexp.MustCompile(`\(SQLSTATE (.*)\)`)
)

var _KlevelToPlevel = map[Level]pgx.LogLevel{
	LvlTrace: pgx.LogLevelTrace,
	LvlDebug: pgx.LogLevelDebug,
	LvlInfo:  pgx.LogLevelInfo,
	LvlWarn:  pgx.LogLevelWarn,
	LvlError: pgx.LogLevelError,
	LvlNone:  pgx.LogLevelNone,
}

type IsolationLevel int

var (
	IsoLvlReadUncommitted IsolationLevel = 0
	IsoLvlReadCommitted   IsolationLevel
	IsoLvlRepeatableRead  IsolationLevel
	IsoLvlSerializable    IsolationLevel
)

var _KisoLevelToPisoLevel = map[IsolationLevel]pgx.TxIsoLevel{
	IsoLvlReadUncommitted: pgx.ReadUncommitted,
	IsoLvlReadCommitted:   pgx.ReadCommitted,
	IsoLvlRepeatableRead:  pgx.RepeatableRead,
	IsoLvlSerializable:    pgx.Serializable,
}

type DatabaseRetryConfig struct {
	Attempts     int
	InitialDelay time.Duration
	LimitDelay   time.Duration
}

type DatabaseConfig struct {
	DatabaseHost                  string
	DatabasePort                  int
	DatabaseSSLMode               string
	DatabaseUser                  string
	DatabasePassword              string
	DatabaseName                  string
	AppName                       string
	DatabaseMinConns              *int
	DatabaseMaxConns              *int
	DatabaseMaxConnIdleTime       *time.Duration
	DatabaseMaxConnLifeTime       *time.Duration
	DatabaseDialTimeout           *time.Duration
	DatabaseStatementTimeout      *time.Duration
	DatabaseLockTimeout           *time.Duration
	DatabaseDefaultIsolationLevel *IsolationLevel
}

type Database struct {
	config   DatabaseConfig
	observer Observer
	pool     *pgxpool.Pool
}

func NewDatabase(ctx context.Context, observer Observer, config DatabaseConfig,
	retry *DatabaseRetryConfig) (*Database, error) {
	if config.DatabaseMinConns == nil {
		config.DatabaseMinConns = util.Pointer(_DATABASE_DEFAULT_MIN_CONNS)
	}

	if config.DatabaseMaxConns == nil {
		config.DatabaseMaxConns = util.Pointer(_DATABASE_DEFAULT_MAX_CONNS)
	}

	if config.DatabaseMaxConnIdleTime == nil {
		config.DatabaseMaxConnIdleTime = util.Pointer(_DATABASE_DEFAULT_MAX_CONN_IDLE_TIME)
	}

	if config.DatabaseMaxConnLifeTime == nil {
		config.DatabaseMaxConnLifeTime = util.Pointer(_DATABASE_DEFAULT_MAX_CONN_LIFE_TIME)
	}

	if config.DatabaseDialTimeout == nil {
		config.DatabaseDialTimeout = util.Pointer(_DATABASE_DEFAULT_DIAL_TIMEOUT)
	}

	if config.DatabaseStatementTimeout == nil {
		config.DatabaseStatementTimeout = util.Pointer(_DATABASE_DEFAULT_STATEMENT_TIMEOUT)
	}

	if config.DatabaseLockTimeout == nil {
		config.DatabaseLockTimeout = util.Pointer(_DATABASE_DEFAULT_LOCK_TIMEOUT)
	}

	if config.DatabaseDefaultIsolationLevel == nil {
		config.DatabaseDefaultIsolationLevel = util.Pointer(_DATABASE_DEFAULT_DEFAULT_ISOLATION_LEVEL)
	}

	if retry == nil {
		retry = &DatabaseRetryConfig{
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
	poolConfig.ConnConfig.ConnectTimeout = *config.DatabaseDialTimeout
	poolConfig.ConnConfig.RuntimeParams["standard_conforming_strings"] = "on"
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.AppName
	poolConfig.ConnConfig.RuntimeParams["default_transaction_isolation"] = string(_KisoLevelToPisoLevel[*config.DatabaseDefaultIsolationLevel])
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.Itoa(int(config.DatabaseStatementTimeout.Milliseconds()))
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = strconv.Itoa(int(config.DatabaseLockTimeout.Milliseconds()))

	pgxLogger := _newPgxLogger(&observer)
	pgxLogLevel := _KlevelToPlevel[pgxLogger.observer.Level()]

	// PGX Info level is too much! (PGX levels are reversed)
	if pgxLogLevel >= pgx.LogLevelInfo {
		pgxLogLevel = pgx.LogLevelError
	}

	poolConfig.ConnConfig.Logger = pgxLogger
	poolConfig.ConnConfig.LogLevel = pgxLogLevel

	var pool *pgxpool.Pool

	err = util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		return util.ExponentialRetry(
			retry.Attempts, retry.InitialDelay, retry.LimitDelay,
			[]error{}, func(attempt int) error {
				var err error // nolint

				observer.Infof(ctx, "Trying to connect to the %s database %d/%d",
					config.DatabaseName, attempt, retry.Attempts)

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
	case util.ErrDeadlineExceeded.Is(err):
		return nil, ErrDatabaseTimedOut()
	default:
		return nil, ErrDatabaseGeneric().Wrap(err)
	}

	observer.Infof(ctx, "Connected to the %s database", config.DatabaseName)

	sqlf.SetDialect(sqlf.PostgreSQL)

	return &Database{
		observer: observer,
		config:   config,
		pool:     pool,
	}, nil
}

func (self *Database) Health(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
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
	case util.ErrDeadlineExceeded.Is(err):
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

	sql := stmt.String()
	args := stmt.Args()
	dest := stmt.Dest()

	ctx, endTraceQuery := self.observer.TraceQuery(ctx, sql, args...)
	defer endTraceQuery()

	var rows pgx.Rows
	var err error

	if ctx.Value(KeyDatabaseTransaction) != nil {
		rows, err = ctx.Value(KeyDatabaseTransaction).(pgx.Tx).Query(ctx, sql, args...)
	} else {
		rows, err = self.pool.Query(ctx, sql, args...)
	}

	if err != nil {
		return _dbErrToError(err)
	}

	err = ctx.Err()
	if err != nil {
		return _dbErrToError(err)
	}

	err = pgxscan.NewScanner(rows).Scan(dest...)
	if err != nil {
		return _dbErrToError(err)
	}

	return nil
}

func (self *Database) Exec(ctx context.Context, stmt *sqlf.Stmt) (int, error) {
	defer stmt.Close()

	sql := stmt.String()
	args := stmt.Args()

	ctx, endTraceQuery := self.observer.TraceQuery(ctx, sql, args...)
	defer endTraceQuery()

	var command pgconn.CommandTag
	var err error

	if ctx.Value(KeyDatabaseTransaction) != nil {
		command, err = ctx.Value(KeyDatabaseTransaction).(pgx.Tx).Exec(ctx, sql, args...)
	} else {
		command, err = self.pool.Exec(ctx, sql, args...)
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

func (self *Database) Transaction(ctx context.Context, level *IsolationLevel, fn func(ctx context.Context) error) error {
	if level == nil {
		level = self.config.DatabaseDefaultIsolationLevel
	}

	if ctx.Value(KeyDatabaseTransaction) != nil {
		err := fn(ctx)
		if err != nil {
			return ErrDatabaseTransactionFailed().WrapAs(err)
		}

		return nil
	}

	transaction, err := self.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   _KisoLevelToPisoLevel[*level],
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
			_ = transaction.Rollback(ctx) // TODO: Combine error
			panic(err)
		}
	}()

	err = fn(context.WithValue(ctx, KeyDatabaseTransaction, transaction))
	if err != nil {
		_ = transaction.Rollback(ctx) // TODO: Combine error
		return ErrDatabaseTransactionFailed().Wrap(err)
	}

	err = ctx.Err()
	if err != nil {
		_ = transaction.Rollback(ctx) // TODO: Combine error
		return ErrDatabaseTransactionFailed().Wrap(err)
	}

	err = transaction.Commit(ctx)
	if err != nil {
		_ = transaction.Rollback(ctx) // TODO: Combine error
		return ErrDatabaseTransactionFailed().Wrap(err)
	}

	err = ctx.Err()
	if err != nil {
		_ = transaction.Rollback(ctx) // TODO: Combine error
		return ErrDatabaseTransactionFailed().Wrap(err)
	}

	return nil
}

func (self *Database) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Infof(ctx, "Closing %s database", self.config.DatabaseName)

		self.pool.Close()

		self.observer.Infof(ctx, "Closed %s database", self.config.DatabaseName)

		return nil
	})
	switch {
	case err == nil:
		return nil
	case util.ErrDeadlineExceeded.Is(err):
		return ErrDatabaseTimedOut()
	default:
		return ErrDatabaseGeneric().Wrap(err)
	}
}

var _PlevelToKlevel = map[pgx.LogLevel]Level{
	pgx.LogLevelTrace: LvlTrace,
	pgx.LogLevelDebug: LvlDebug,
	pgx.LogLevelInfo:  LvlInfo,
	pgx.LogLevelWarn:  LvlWarn,
	pgx.LogLevelError: LvlError,
	pgx.LogLevelNone:  LvlNone,
}

type _pgxLogger struct {
	observer *Observer
}

func _newPgxLogger(observer *Observer) *_pgxLogger {
	return &_pgxLogger{
		observer: observer,
	}
}

func (self _pgxLogger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]any) { // nolint
	self.observer.WithLevelf(ctx, _PlevelToKlevel[level], "%s: %+v", msg, data)
}
