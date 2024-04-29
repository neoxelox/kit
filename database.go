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
	"github.com/neoxelox/errors"
	"github.com/randallmlough/pgxscan"

	"github.com/neoxelox/kit/util"
)

const (
	_DATABASE_POSTGRES_DSN = "postgresql://%s:%s@%s:%d/%s?sslmode=%s"
)

var (
	_DATABASE_ERR_PGCODE = regexp.MustCompile(`\(SQLSTATE (.*)\)`)

	KeyDatabaseTransaction Key = KeyBase + "database:transaction"
)

var (
	ErrDatabaseGeneric            = errors.New("database failed")
	ErrDatabaseTimedOut           = errors.New("database timed out")
	ErrDatabaseUnhealthy          = errors.New("database unhealthy")
	ErrDatabaseTransactionFailed  = errors.New("database transaction failed")
	ErrDatabaseNoRows             = errors.New("database no rows in result set")
	ErrDatabaseIntegrityViolation = errors.New("database integrity constraint violation")
	ErrDatabaseUnexpectedEffect   = errors.New("database affected %d out of %d expected rows")
)

var _KlevelToPlevel = map[Level]pgx.LogLevel{
	LvlTrace: pgx.LogLevelTrace,
	LvlDebug: pgx.LogLevelDebug,
	LvlInfo:  pgx.LogLevelInfo,
	LvlWarn:  pgx.LogLevelWarn,
	LvlError: pgx.LogLevelError,
	LvlNone:  pgx.LogLevelNone,
}

var (
	_DATABASE_DEFAULT_CONFIG = DatabaseConfig{
		MinConns:              util.Pointer(1),
		MaxConns:              util.Pointer(max(4, 2*runtime.GOMAXPROCS(-1))),
		MaxConnIdleTime:       util.Pointer(30 * time.Minute),
		MaxConnLifeTime:       util.Pointer(1 * time.Hour),
		DialTimeout:           util.Pointer(30 * time.Second),
		StatementTimeout:      util.Pointer(30 * time.Second),
		DefaultIsolationLevel: util.Pointer(IsoLvlReadCommitted),
	}

	_DATABASE_DEFAULT_RETRY_CONFIG = RetryConfig{
		Attempts:     1,
		InitialDelay: 0 * time.Second,
		LimitDelay:   0 * time.Second,
		Retriables:   []error{},
	}
)

type IsolationLevel int

var (
	IsoLvlReadUncommitted IsolationLevel = 0
	IsoLvlReadCommitted   IsolationLevel = 1
	IsoLvlRepeatableRead  IsolationLevel = 2
	IsoLvlSerializable    IsolationLevel = 3
)

var _KisoLevelToPisoLevel = map[IsolationLevel]pgx.TxIsoLevel{
	IsoLvlReadUncommitted: pgx.ReadUncommitted,
	IsoLvlReadCommitted:   pgx.ReadCommitted,
	IsoLvlRepeatableRead:  pgx.RepeatableRead,
	IsoLvlSerializable:    pgx.Serializable,
}

type DatabaseConfig struct {
	Host                  string
	Port                  int
	SSLMode               string
	User                  string
	Password              string
	Database              string
	Service               string
	MinConns              *int
	MaxConns              *int
	MaxConnIdleTime       *time.Duration
	MaxConnLifeTime       *time.Duration
	DialTimeout           *time.Duration
	StatementTimeout      *time.Duration
	DefaultIsolationLevel *IsolationLevel
}

type Database struct {
	config   DatabaseConfig
	observer *Observer
	pool     *pgxpool.Pool
}

func NewDatabase(ctx context.Context, observer *Observer, config DatabaseConfig,
	retry ...RetryConfig) (*Database, error) {
	util.Merge(&config, _DATABASE_DEFAULT_CONFIG)
	_retry := util.Optional(retry, _DATABASE_DEFAULT_RETRY_CONFIG)

	dsn := fmt.Sprintf(
		_DATABASE_POSTGRES_DSN,
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.Database,
		config.SSLMode,
	)

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, ErrDatabaseGeneric.Raise().Cause(err)
	}

	poolConfig.MinConns = int32(*config.MinConns)
	poolConfig.MaxConns = int32(*config.MaxConns)
	poolConfig.MaxConnIdleTime = *config.MaxConnIdleTime
	poolConfig.MaxConnLifetime = *config.MaxConnLifeTime
	poolConfig.ConnConfig.ConnectTimeout = *config.DialTimeout
	poolConfig.ConnConfig.RuntimeParams["standard_conforming_strings"] = "on"
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.Service
	poolConfig.ConnConfig.RuntimeParams["default_transaction_isolation"] = string(
		_KisoLevelToPisoLevel[*config.DefaultIsolationLevel])
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.Itoa(int(config.StatementTimeout.Milliseconds()))
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = strconv.Itoa(int(config.StatementTimeout.Milliseconds()))

	pgxLogger := _newPgxLogger(observer)
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
			_retry.Attempts, _retry.InitialDelay, _retry.LimitDelay,
			_retry.Retriables, func(attempt int) error {
				var err error // nolint:govet

				observer.Infof(ctx, "Trying to connect to the %s database %d/%d",
					config.Database, attempt, _retry.Attempts)

				pool, err = pgxpool.ConnectConfig(ctx, poolConfig)
				if err != nil {
					return ErrDatabaseGeneric.Raise().Cause(err)
				}

				err = pool.Ping(ctx)
				if err != nil {
					return ErrDatabaseGeneric.Raise().Cause(err)
				}

				return nil
			})
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return nil, ErrDatabaseTimedOut.Raise().Cause(err)
		}

		return nil, err
	}

	observer.Infof(ctx, "Connected to the %s database", config.Database)

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
		if currentConns < int32(*self.config.MinConns) {
			return ErrDatabaseUnhealthy.Raise().With("current conns %d below minimum %d",
				currentConns, *self.config.MinConns)
		}

		err := self.pool.Ping(ctx)
		if err != nil {
			return ErrDatabaseUnhealthy.Raise().Cause(err)
		}

		err = ctx.Err()
		if err != nil {
			return ErrDatabaseUnhealthy.Raise().Cause(err)
		}

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrDatabaseTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}

func _dbErrToError(err error) *errors.Error {
	if err == nil {
		return nil
	}

	if code := _DATABASE_ERR_PGCODE.FindStringSubmatch(err.Error()); len(code) == 2 {
		switch code[1] {
		case pgerrcode.IntegrityConstraintViolation, pgerrcode.RestrictViolation, pgerrcode.NotNullViolation,
			pgerrcode.ForeignKeyViolation, pgerrcode.UniqueViolation, pgerrcode.CheckViolation,
			pgerrcode.ExclusionViolation:
			return ErrDatabaseIntegrityViolation.Raise().Skip(2).Cause(err)
		}
	}

	switch err {
	case pgx.ErrNoRows:
		return ErrDatabaseNoRows.Raise().Skip(2).Cause(err)
	default:
		return ErrDatabaseGeneric.Raise().Skip(2).Cause(err)
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

func (self *Database) Transaction(
	ctx context.Context, level *IsolationLevel, fn func(ctx context.Context) error) error {
	if level == nil {
		level = self.config.DefaultIsolationLevel
	}

	if ctx.Value(KeyDatabaseTransaction) != nil {
		err := fn(ctx)
		if err != nil {
			// Wait to rollback context transaction at the original Transaction call
			return ErrDatabaseTransactionFailed.Raise().Cause(err)
		}

		return nil
	}

	transaction, err := self.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   _KisoLevelToPisoLevel[*level],
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		// Don't rollback transaction because it was not created
		return ErrDatabaseTransactionFailed.Raise().Cause(err)
	}

	err = ctx.Err()
	if err != nil {
		errT := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed.Raise().Extra(map[string]any{"transaction_error": errT}).Cause(err)
	}

	defer func() {
		rec := recover()
		if rec != nil {
			errT := transaction.Rollback(ctx)

			err, ok := rec.(error) // nolint:govet
			if !ok {
				err = ErrDatabaseGeneric.Raise().With("%v", rec)
			}

			// Repanic so upwards middlewares are aware of it
			panic(ErrDatabaseTransactionFailed.Raise().Extra(map[string]any{"transaction_error": errT}).Cause(err))
		}
	}()

	err = fn(context.WithValue(ctx, KeyDatabaseTransaction, transaction))
	if err != nil {
		errT := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed.Raise().Extra(map[string]any{"transaction_error": errT}).Cause(err)
	}

	err = ctx.Err()
	if err != nil {
		errT := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed.Raise().Extra(map[string]any{"transaction_error": errT}).Cause(err)
	}

	err = transaction.Commit(ctx)
	if err != nil {
		errT := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed.Raise().Extra(map[string]any{"transaction_error": errT}).Cause(err)
	}

	err = ctx.Err()
	if err != nil {
		errT := transaction.Rollback(ctx)
		return ErrDatabaseTransactionFailed.Raise().Extra(map[string]any{"transaction_error": errT}).Cause(err)
	}

	return nil
}

func (self *Database) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Infof(ctx, "Closing %s database", self.config.Database)

		self.pool.Close()

		self.observer.Infof(ctx, "Closed %s database", self.config.Database)

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrDatabaseTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
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

func (self _pgxLogger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]any) {
	self.observer.WithLevelf(ctx, _PlevelToKlevel[level], "%s: %+v", msg, data)
}
