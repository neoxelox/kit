package kit

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/jackc/pgx/v4"
	gommon "github.com/labstack/gommon/log"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
)

const (
	_LOGGER_LEVEL_FIELD_NAME     = "level"
	_LOGGER_MESSAGE_FIELD_NAME   = "message"
	_LOGGER_APP_FIELD_NAME       = "app"
	_LOGGER_FILE_FIELD_NAME      = "file"
	_LOGGER_TIMESTAMP_FIELD_NAME = "timestamp"
	_LOGGER_WRITER_SIZE          = 1000
	_LOGGER_POLL_INTERVAL        = 10 * time.Millisecond
	_LOGGER_FLUSH_DELAY          = _LOGGER_POLL_INTERVAL * 10
)

var (
	_LOGGER_DEFAULT_DEV_LEVEL  = zerolog.DebugLevel
	_LOGGER_DEFAULT_PROD_LEVEL = zerolog.InfoLevel
)

type LoggerConfig struct {
	Environment string
	AppName     string
	Level       *zerolog.Level
}

type Logger struct {
	logger  zerolog.Logger
	config  LoggerConfig
	out     io.Writer
	prefix  string
	header  string
	level   zerolog.Level
	verbose bool
}

func NewLogger(config LoggerConfig) *Logger {
	if config.Level == nil {
		if config.Environment != Environments.Development {
			config.Level = &_LOGGER_DEFAULT_PROD_LEVEL
		} else {
			config.Level = &_LOGGER_DEFAULT_DEV_LEVEL
		}
	}

	zerolog.LevelFieldName = _LOGGER_LEVEL_FIELD_NAME
	zerolog.MessageFieldName = _LOGGER_MESSAGE_FIELD_NAME
	zerolog.TimestampFieldName = _LOGGER_TIMESTAMP_FIELD_NAME
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		file = "logger.go"
	}

	out := diode.NewWriter(os.Stdout, _LOGGER_WRITER_SIZE, _LOGGER_POLL_INTERVAL, func(missed int) {
		fmt.Fprintf(os.Stdout,
			"{\"%s\":\"%s\",\"%s\":\"%s\",\"%s\":\"%s\",\"%s\":%d,\"%s\":\"Logger dropped %d messages\"}\n",
			zerolog.LevelFieldName, zerolog.InfoLevel, _LOGGER_APP_FIELD_NAME,
			config.AppName, _LOGGER_FILE_FIELD_NAME, file, zerolog.TimestampFieldName,
			time.Now().Unix(), zerolog.MessageFieldName, missed)
	})

	// Do not use Caller hook as runtime.Caller makes the logger up to 2.6x slower
	return &Logger{
		logger: zerolog.New(out).With().
			Str(_LOGGER_APP_FIELD_NAME, config.AppName).
			Timestamp().
			Logger().
			Level(*config.Level),
		config:  config,
		level:   *config.Level,
		out:     out,
		prefix:  config.AppName,
		header:  "",
		verbose: *config.Level == zerolog.DebugLevel,
	}
}

func (self Logger) Logger() *zerolog.Logger {
	return &self.logger
}

func (self *Logger) SetLogger(l zerolog.Logger) {
	self.logger = l
}

func (self Logger) Flush(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		time.Sleep(_LOGGER_FLUSH_DELAY)
		os.Stdout.Sync()
		os.Stderr.Sync()

		return nil
	})
	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrLoggerTimedOut()
	default:
		return Errors.ErrLoggerGeneric().Wrap(err)
	}
}

func (self Logger) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.logger.Info().Msg("Closing logger")

		err := self.Flush(ctx)
		if err != nil {
			return Errors.ErrLoggerGeneric().WrapAs(err)
		}

		writer, ok := self.out.(diode.Writer)
		if !ok {
			panic("logger writer is not diode")
		}

		err = writer.Close()
		if err != nil {
			return Errors.ErrLoggerGeneric().WrapAs(err)
		}

		self.logger.Info().Msg("Closed logger")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case Errors.ErrDeadlineExceeded().Is(err):
		return Errors.ErrLoggerTimedOut()
	default:
		return Errors.ErrLoggerGeneric().Wrap(err)
	}
}

// TODO: dedup file field
func (self *Logger) SetFile(frames int) {
	if _, file, _, ok := runtime.Caller(frames); ok {
		self.logger = self.logger.With().Str(_LOGGER_FILE_FIELD_NAME, file).Logger()
	}
}

func (self Logger) Output() io.Writer {
	return self.out
}

func (self *Logger) SetOutput(w io.Writer) {
	self.logger = self.logger.Output(w)
	self.out = w
}

func (self Logger) Prefix() string {
	return self.prefix
}

func (self *Logger) SetPrefix(p string) {
	self.prefix = p
}

func (self Logger) GLevel() gommon.Lvl {
	return Utils.ZlevelToGlevel[self.level]
}

func (self *Logger) SetGLevel(l gommon.Lvl) {
	zlevel := Utils.GlevelToZlevel[l]
	self.logger = self.logger.Level(zlevel)
	self.level = zlevel
}

func (self Logger) PLevel() pgx.LogLevel {
	return Utils.ZlevelToPlevel[self.level]
}

func (self *Logger) SetPLevel(l pgx.LogLevel) {
	zlevel := Utils.PlevelToZlevel[l]
	self.logger = self.logger.Level(zlevel)
	self.level = zlevel
}

func (self Logger) ZLevel() zerolog.Level {
	return self.level
}

func (self *Logger) SetZLevel(l zerolog.Level) {
	zlevel := l
	self.logger = self.logger.Level(zlevel)
	self.level = zlevel
}

func (self *Logger) Header() string {
	return self.header
}

func (self *Logger) SetHeader(h string) {
	self.header = h
}

func (self Logger) Verbose() bool {
	return self.verbose
}

func (self *Logger) SetVerbose(v bool) {
	self.verbose = v
}

func (self Logger) Print(i ...interface{}) {
	self.logger.Log().Msg(fmt.Sprint(i...))
}

func (self Logger) Printf(format string, i ...interface{}) {
	self.logger.Log().Msgf(format, i...)
}

func (self Logger) Debug(i ...interface{}) {
	self.logger.Debug().Msg(fmt.Sprint(i...))
}

func (self Logger) Debugf(format string, i ...interface{}) {
	self.logger.Debug().Msgf(format, i...)
}

func (self Logger) Info(i ...interface{}) {
	self.logger.Info().Msg(fmt.Sprint(i...))
}

func (self Logger) Infof(format string, i ...interface{}) {
	self.logger.Info().Msgf(format, i...)
}

func (self Logger) Warn(i ...interface{}) {
	self.logger.Warn().Msg(fmt.Sprint(i...))
}

func (self Logger) Warnf(format string, i ...interface{}) {
	self.logger.Warn().Msgf(format, i...)
}

func (self Logger) Error(i ...interface{}) {
	if self.config.Environment == Environments.Production {
		self.logger.Error().Msg(fmt.Sprint(i...))
		return
	}

	for _, ie := range i {
		switch err := ie.(type) {
		case nil:
		case *Error:
			fmt.Printf("%+v\n", err.Unwrap()) // nolint
		case *Exception:
			fmt.Printf("%+v\n", err.Unwrap()) // nolint
		default:
			fmt.Printf("%+v\n", err) // nolint
		}
	}
}

func (self Logger) Errorf(format string, i ...interface{}) {
	self.logger.Error().Msgf(format, i...)
}

func (self Logger) Fatal(i ...interface{}) {
	self.logger.Fatal().Msg(fmt.Sprint(i...))
}

func (self Logger) Fatalf(format string, i ...interface{}) {
	self.logger.Fatal().Msgf(format, i...)
}

func (self Logger) Panic(i ...interface{}) {
	self.logger.Panic().Msg(fmt.Sprint(i...))
}

func (self Logger) Panicf(format string, i ...interface{}) {
	self.logger.Panic().Msgf(format, i...)
}
