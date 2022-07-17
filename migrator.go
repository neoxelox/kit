package kit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	_MIGRATOR_DEFAULT_MIGRATIONS_PATH = "./migrations"
	_MIGRATOR_POSTGRES_DSN            = "postgresql://%s:%s@%s:%d/%s?sslmode=%s&x-multi-statement=true"
)

type MigratorConfig struct {
	MigrationsPath   *string
	SchemaVersion    int
	DatabaseHost     string
	DatabasePort     int
	DatabaseSSLMode  string
	DatabaseUser     string
	DatabasePassword string
	DatabaseName     string
	Timeout          *int
}

type Migrator struct {
	config   MigratorConfig
	logger   Logger
	migrator migrate.Migrate
	done     chan bool
}

func NewMigrator(logger Logger, config MigratorConfig) (*Migrator, error) {
	logger.SetLogger(logger.Logger().With().Str("layer", "migrator").Logger())

	migrationsPath := _MIGRATOR_DEFAULT_MIGRATIONS_PATH
	if config.MigrationsPath != nil {
		migrationsPath = *config.MigrationsPath
	}

	migrationsPath = fmt.Sprintf("file://%s", migrationsPath)

	dsn := fmt.Sprintf(
		_MIGRATOR_POSTGRES_DSN,
		config.DatabaseUser,
		config.DatabasePassword,
		config.DatabaseHost,
		config.DatabasePort,
		config.DatabaseName,
		config.DatabaseSSLMode,
	)

	migrator, err := migrate.New(migrationsPath, dsn)
	if err != nil {
		return nil, Errors.ErrMigratorGeneric().Wrap(err)
	}

	migrator.Log = *newMigrateLogger(logger)
	if config.Timeout != nil {
		migrator.LockTimeout = time.Duration(*config.Timeout) * time.Second
	}

	return &Migrator{
		logger:   logger,
		config:   config,
		migrator: *migrator,
		done:     make(chan bool, 1),
	}, nil
}

func (self *Migrator) Apply(ctx context.Context) error {
	var err error

	if self.config.Timeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*self.config.Timeout)*time.Second)

		defer cancel()
	}

	go func() {
		err = func() error {
			schemaVersion, bad, err := self.migrator.Version() // nolint
			if err != nil && err != migrate.ErrNilVersion {
				return Errors.ErrMigratorGeneric().Wrap(err)
			}

			if bad {
				return Errors.ErrMigratorGeneric().Withf("current schema version %d is dirty", schemaVersion)
			}

			if schemaVersion == uint(self.config.SchemaVersion) {
				self.logger.Info("No migrations to apply")

				return nil
			}

			if schemaVersion > uint(self.config.SchemaVersion) {
				return Errors.ErrMigratorGeneric().Withf("desired schema version %d behind from current one %d",
					self.config.SchemaVersion, schemaVersion)
			}

			self.logger.Infof("%d migrations to be applied", self.config.SchemaVersion-int(schemaVersion))

			err = self.migrator.Migrate(uint(self.config.SchemaVersion))
			if err != nil {
				return Errors.ErrMigratorGeneric().Wrap(err)
			}

			self.logger.Info("Applied all migrations successfully")

			return nil
		}()

		close(self.done)
	}()
	select {
	case <-ctx.Done():
		err = Errors.ErrMigratorTimedOut().Wrap(err)
	case <-self.done:
	}

	errC := self.Close(ctx)

	self.done = make(chan bool, 1)

	err = Utils.CombineErrors(err, errC)
	if err != nil {
		return Errors.ErrMigratorGeneric().Wrap(err)
	}

	return nil
}

func (self *Migrator) Rollback(ctx context.Context) error {
	var err error

	if self.config.Timeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*self.config.Timeout)*time.Second)

		defer cancel()
	}

	go func() {
		err = func() error {
			schemaVersion, bad, err := self.migrator.Version() // nolint
			if err != nil {
				return Errors.ErrMigratorGeneric().Wrap(err)
			}

			if bad {
				self.logger.Infof("Current schema version %d is dirty, ignoring", schemaVersion)

				err = self.migrator.Force(int(schemaVersion))
				if err != nil {
					return Errors.ErrMigratorGeneric().Wrap(err)
				}
			}

			if schemaVersion == uint(self.config.SchemaVersion) {
				self.logger.Info("No migrations to rollback")

				return nil
			}

			if schemaVersion < uint(self.config.SchemaVersion) {
				return Errors.ErrMigratorGeneric().Withf("desired schema version %d ahead of current one %d",
					self.config.SchemaVersion, schemaVersion)
			}

			self.logger.Infof("%d migrations to be rollbacked", int(schemaVersion)-self.config.SchemaVersion)

			err = self.migrator.Migrate(uint(self.config.SchemaVersion))
			if err != nil {
				return Errors.ErrMigratorGeneric().Wrap(err)
			}

			self.logger.Info("Rollbacked all migrations successfully")

			return nil
		}()

		close(self.done)
	}()
	select {
	case <-ctx.Done():
		err = Errors.ErrMigratorTimedOut().Wrap(err)
	case <-self.done:
	}

	errC := self.Close(ctx)

	self.done = make(chan bool, 1)

	err = Utils.CombineErrors(err, errC)
	if err != nil {
		return Errors.ErrMigratorGeneric().Wrap(err)
	}

	return nil
}

func (self *Migrator) Close(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		self.logger.Logger().Info().Time("deadline", deadline).Msg("Closing migrator")
	} else {
		self.logger.Info("Closing migrator")
	}

	self.migrator.GracefulStop <- true

	<-self.done

	err, errD := self.migrator.Close()

	err = Utils.CombineErrors(err, errD)
	if err != nil {
		return Errors.ErrMigratorGeneric().Wrap(err)
	}

	return nil
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
