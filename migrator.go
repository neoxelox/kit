package kit

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const _MIGRATOR_POSTGRES_DSN = "postgresql://%s:%s@%s:%d/%s?sslmode=%s&x-multi-statement=true"

var (
	_MIGRATOR_DEFAULT_MIGRATIONS_PATH     = "./migrations"
	_MIGRATOR_DEFAULT_RETRY_ATTEMPTS      = 1
	_MIGRATOR_DEFAULT_RETRY_INITIAL_DELAY = 0 * time.Second
	_MIGRATOR_DEFAULT_RETRY_LIMIT_DELAY   = 0 * time.Second
	_MIGRATOR_ERR_DB_ALREADY_CLOSED       = regexp.MustCompile(`.*connection is already closed.*`)
)

type MigratorRetryConfig struct {
	Attempts     int
	InitialDelay time.Duration
	LimitDelay   time.Duration
}

type MigratorConfig struct {
	MigrationsPath   *string
	DatabaseHost     string
	DatabasePort     int
	DatabaseSSLMode  string
	DatabaseUser     string
	DatabasePassword string
	DatabaseName     string
	RetryConfig      *MigratorRetryConfig
}

type Migrator struct {
	config   MigratorConfig
	observer Observer
	migrator migrate.Migrate
	done     chan struct{}
}

func NewMigrator(ctx context.Context, observer Observer, config MigratorConfig) (*Migrator, error) {
	observer.Anchor()

	if config.MigrationsPath == nil {
		config.MigrationsPath = &_MIGRATOR_DEFAULT_MIGRATIONS_PATH
	}

	*config.MigrationsPath = fmt.Sprintf("file://%s", filepath.Clean(*config.MigrationsPath))

	if config.RetryConfig == nil {
		config.RetryConfig = &MigratorRetryConfig{
			Attempts:     _MIGRATOR_DEFAULT_RETRY_ATTEMPTS,
			InitialDelay: _MIGRATOR_DEFAULT_RETRY_INITIAL_DELAY,
			LimitDelay:   _MIGRATOR_DEFAULT_RETRY_LIMIT_DELAY,
		}
	}

	dsn := fmt.Sprintf(
		_MIGRATOR_POSTGRES_DSN,
		config.DatabaseUser,
		config.DatabasePassword,
		config.DatabaseHost,
		config.DatabasePort,
		config.DatabaseName,
		config.DatabaseSSLMode,
	)

	var migrator *migrate.Migrate

	// TODO: only retry on specific errors
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		return Utils.ExponentialRetry(
			config.RetryConfig.Attempts, config.RetryConfig.InitialDelay, config.RetryConfig.LimitDelay,
			nil, func(attempt int) error {
				var err error

				observer.Infof("Trying to connect to the database %d/%d", attempt, config.RetryConfig.Attempts)

				migrator, err = migrate.New(*config.MigrationsPath, dsn)
				if err != nil {
					return Errors.ErrMigratorGeneric().WrapAs(err)
				}

				return nil
			})
	})
	switch {
	case err == nil:
	case Errors.ErrDeadlineExceeded().Is(err):
		return nil, Errors.ErrMigratorTimedOut()
	default:
		return nil, Errors.ErrMigratorGeneric().Wrap(err)
	}

	observer.Info("Connected to the database")

	migrator.Log = *newMigrateLogger(observer.Logger)

	done := make(chan struct{}, 1)
	close(done)

	return &Migrator{
		observer: observer,
		config:   config,
		migrator: *migrator,
		done:     done,
	}, nil
}

// TODO: concurrent-safe
func (self *Migrator) Assert(ctx context.Context, schemaVersion int) error {
	self.done = make(chan struct{}, 1)

	if ctxDeadline, ok := ctx.Deadline(); ok {
		self.migrator.LockTimeout = time.Until(ctxDeadline)
	}

	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		err := func() error {
			currentSchemaVersion, bad, err := self.migrator.Version() // nolint
			if err != nil && err != migrate.ErrNilVersion {
				return Errors.ErrMigratorGeneric().WrapAs(err)
			}

			if bad {
				return Errors.ErrMigratorGeneric().Withf("current schema version %d is dirty", currentSchemaVersion)
			}

			if currentSchemaVersion > uint(schemaVersion) {
				return Errors.ErrMigratorGeneric().Withf("desired schema version %d behind from current one %d",
					schemaVersion, currentSchemaVersion)
			} else if currentSchemaVersion < uint(schemaVersion) {
				return Errors.ErrMigratorGeneric().Withf("desired schema version %d ahead of current one %d",
					schemaVersion, currentSchemaVersion)
			}

			self.observer.Infof("Desired schema version %d asserted", schemaVersion)

			return nil
		}()

		select {
		case <-self.done:
		default:
			close(self.done)
		}

		return err
	})

	self.migrator.LockTimeout = migrate.DefaultLockTimeout

	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrMigratorTimedOut()
	default:
		return Errors.ErrMigratorGeneric().Wrap(err)
	}
}

// TODO: concurrent-safe
func (self *Migrator) Apply(ctx context.Context, schemaVersion int) error {
	self.done = make(chan struct{}, 1)

	if ctxDeadline, ok := ctx.Deadline(); ok {
		self.migrator.LockTimeout = time.Until(ctxDeadline)
	}

	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		err := func() error {
			currentSchemaVersion, bad, err := self.migrator.Version() // nolint
			if err != nil && err != migrate.ErrNilVersion {
				return Errors.ErrMigratorGeneric().WrapAs(err)
			}

			if bad {
				return Errors.ErrMigratorGeneric().Withf("current schema version %d is dirty", currentSchemaVersion)
			}

			if currentSchemaVersion == uint(schemaVersion) {
				self.observer.Info("No migrations to apply")

				return nil
			}

			if currentSchemaVersion > uint(schemaVersion) {
				return Errors.ErrMigratorGeneric().Withf("desired schema version %d behind from current one %d",
					schemaVersion, currentSchemaVersion)
			}

			self.observer.Infof("%d migrations to be applied", schemaVersion-int(currentSchemaVersion))

			err = self.migrator.Migrate(uint(schemaVersion))
			if err != nil {
				return Errors.ErrMigratorGeneric().WrapAs(err)
			}

			self.observer.Info("Applied all migrations successfully")

			return nil
		}()

		select {
		case <-self.done:
		default:
			close(self.done)
		}

		return err
	})

	self.migrator.LockTimeout = migrate.DefaultLockTimeout

	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrMigratorTimedOut()
	default:
		return Errors.ErrMigratorGeneric().Wrap(err)
	}
}

// TODO: concurrent-safe
func (self *Migrator) Rollback(ctx context.Context, schemaVersion int) error {
	self.done = make(chan struct{}, 1)

	if ctxDeadline, ok := ctx.Deadline(); ok {
		self.migrator.LockTimeout = time.Until(ctxDeadline)
	}

	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		err := func() error {
			currentSchemaVersion, bad, err := self.migrator.Version() // nolint
			if err != nil {
				return Errors.ErrMigratorGeneric().WrapAs(err)
			}

			if bad {
				self.observer.Infof("Current schema version %d is dirty, ignoring", currentSchemaVersion)

				err = self.migrator.Force(int(currentSchemaVersion))
				if err != nil {
					return Errors.ErrMigratorGeneric().WrapAs(err)
				}
			}

			if currentSchemaVersion == uint(schemaVersion) {
				self.observer.Info("No migrations to rollback")

				return nil
			}

			if currentSchemaVersion < uint(schemaVersion) {
				return Errors.ErrMigratorGeneric().Withf("desired schema version %d ahead of current one %d",
					schemaVersion, currentSchemaVersion)
			}

			self.observer.Infof("%d migrations to be rollbacked", int(currentSchemaVersion)-schemaVersion)

			err = self.migrator.Migrate(uint(schemaVersion))
			if err != nil {
				return Errors.ErrMigratorGeneric().WrapAs(err)
			}

			self.observer.Info("Rollbacked all migrations successfully")

			return nil
		}()

		select {
		case <-self.done:
		default:
			close(self.done)
		}

		return err
	})

	self.migrator.LockTimeout = migrate.DefaultLockTimeout

	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrMigratorTimedOut()
	default:
		return Errors.ErrMigratorGeneric().Wrap(err)
	}
}

func (self *Migrator) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.observer.Info("Closing migrator")

		select {
		case self.migrator.GracefulStop <- true:
		default:
		}

		<-self.done

		err, errD := self.migrator.Close()
		if errD != nil && _MIGRATOR_ERR_DB_ALREADY_CLOSED.MatchString(errD.Error()) {
			errD = nil
		}

		err = Utils.CombineErrors(err, errD)
		if err != nil {
			return Errors.ErrMigratorGeneric().WrapAs(err)
		}

		self.observer.Info("Closed migrator")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrMigratorTimedOut()
	default:
		return Errors.ErrMigratorGeneric().Wrap(err)
	}
}

type _migrateLogger struct {
	logger Logger
}

func newMigrateLogger(logger Logger) *_migrateLogger {
	return &_migrateLogger{
		logger: logger,
	}
}

func (self _migrateLogger) Printf(format string, v ...interface{}) {
	format = strings.TrimSpace(format)
	self.logger.Infof(format, v...)
}

func (self _migrateLogger) Verbose() bool {
	return false
}
