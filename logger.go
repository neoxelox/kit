package kit

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
)

const (
	_LOGGER_LEVEL_FIELD_NAME       = "level"
	_LOGGER_MESSAGE_FIELD_NAME     = "message"
	_LOGGER_APP_FIELD_NAME         = "app"
	_LOGGER_FILE_FIELD_NAME        = "file"
	_LOGGER_TIMESTAMP_FIELD_NAME   = "timestamp"
	_LOGGER_TIMESTAMP_FIELD_FORMAT = zerolog.TimeFormatUnix
	_LOGGER_WRITER_SIZE            = 1000
	_LOGGER_POLL_INTERVAL          = 10 * time.Millisecond
	_LOGGER_FLUSH_DELAY            = _LOGGER_POLL_INTERVAL * 10
)

var _KlevelToZlevel = map[_level]zerolog.Level{
	LvlTrace: zerolog.TraceLevel,
	LvlDebug: zerolog.DebugLevel,
	LvlInfo:  zerolog.InfoLevel,
	LvlWarn:  zerolog.WarnLevel,
	LvlError: zerolog.ErrorLevel,
	LvlNone:  zerolog.Disabled,
}

type LoggerConfig struct {
	AppName string
	Level   _level
}

type Logger struct {
	logger  *zerolog.Logger
	config  LoggerConfig
	out     io.Writer
	prefix  string
	header  string
	file    string
	level   _level
	verbose bool
}

func NewLogger(config LoggerConfig) *Logger {
	zerolog.LevelFieldName = _LOGGER_LEVEL_FIELD_NAME
	zerolog.MessageFieldName = _LOGGER_MESSAGE_FIELD_NAME
	zerolog.TimestampFieldName = _LOGGER_TIMESTAMP_FIELD_NAME
	zerolog.TimeFieldFormat = _LOGGER_TIMESTAMP_FIELD_FORMAT

	_, file, _, _ := runtime.Caller(0)

	out := diode.NewWriter(os.Stdout, _LOGGER_WRITER_SIZE, _LOGGER_POLL_INTERVAL, func(missed int) {
		fmt.Fprintf(os.Stdout,
			"{\"%s\":\"%s\",\"%s\":\"%s\",\"%s\":\"%s\",\"%s\":%d,\"%s\":\"Logger dropped %d messages\"}\n",
			zerolog.LevelFieldName, zerolog.ErrorLevel, _LOGGER_APP_FIELD_NAME,
			config.AppName, _LOGGER_FILE_FIELD_NAME, file, zerolog.TimestampFieldName,
			time.Now().Unix(), zerolog.MessageFieldName, missed)
	})

	// Do not use Caller hook as runtime.Caller makes the logger up to 2.6x slower
	logger := zerolog.New(out).With().
		Str(_LOGGER_APP_FIELD_NAME, config.AppName).
		Timestamp().
		Logger().
		Level(_KlevelToZlevel[config.Level])

	return &Logger{
		logger:  &logger,
		config:  config,
		level:   config.Level,
		out:     &out,
		prefix:  config.AppName,
		header:  "",
		file:    "",
		verbose: LvlDebug >= config.Level,
	}
}

func (self Logger) Logger() *zerolog.Logger {
	return self.logger
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
	case ErrDeadlineExceeded().Is(err):
		return ErrLoggerTimedOut()
	default:
		return ErrLoggerGeneric().Wrap(err)
	}
}

func (self Logger) Close(ctx context.Context) error {
	err := Utils.Deadline(ctx, func(exceeded <-chan struct{}) error {
		_, file, _, _ := runtime.Caller(0)

		self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Closing logger")

		err := self.Flush(ctx)
		if err != nil {
			return ErrLoggerGeneric().WrapAs(err)
		}

		writer, ok := self.out.(*diode.Writer)
		if !ok {
			panic("logger writer is not diode")
		}

		err = writer.Close()
		if err != nil {
			return ErrLoggerGeneric().WrapAs(err)
		}

		self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, file).Msg("Closed logger")

		return nil
	})
	switch {
	case err == nil:
		return nil
	case ErrDeadlineExceeded().Is(err):
		return ErrLoggerTimedOut()
	default:
		return ErrLoggerGeneric().Wrap(err)
	}
}

func (self Logger) File() string {
	return self.file
}

func (self *Logger) SetFile(frames int) {
	if _, file, _, ok := runtime.Caller(frames + 1); ok {
		self.file = file
	}
}

func (self Logger) Output() io.Writer {
	return self.out
}

func (self *Logger) SetOutput(w io.Writer) {
	*self.logger = self.logger.Output(w)
	self.out = w
}

func (self Logger) Prefix() string {
	return self.prefix
}

func (self *Logger) SetPrefix(p string) {
	self.prefix = p
}

func (self Logger) Level() _level { // nolint
	return self.level
}

func (self *Logger) SetLevel(l _level) {
	*self.logger = self.logger.Level(_KlevelToZlevel[l])
	self.level = l
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
	self.logger.Log().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
}

func (self Logger) Printf(format string, i ...interface{}) {
	self.logger.Log().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}

func (self Logger) Debug(i ...interface{}) {
	self.logger.Debug().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
}

func (self Logger) Debugf(format string, i ...interface{}) {
	self.logger.Debug().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}

func (self Logger) Info(i ...interface{}) {
	self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
}

func (self Logger) Infof(format string, i ...interface{}) {
	self.logger.Info().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}

func (self Logger) Warn(i ...interface{}) {
	self.logger.Warn().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
}

func (self Logger) Warnf(format string, i ...interface{}) {
	self.logger.Warn().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}

func (self Logger) Error(i ...interface{}) {
	if LvlDebug >= self.level {
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
	} else {
		self.logger.Error().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
	}
}

func (self Logger) Errorf(format string, i ...interface{}) {
	self.logger.Error().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}

func (self Logger) Fatal(i ...interface{}) {
	self.logger.Fatal().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
}

func (self Logger) Fatalf(format string, i ...interface{}) {
	self.logger.Fatal().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}

func (self Logger) Panic(i ...interface{}) {
	self.logger.Panic().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msg(fmt.Sprint(i...))
}

func (self Logger) Panicf(format string, i ...interface{}) {
	self.logger.Panic().Str(_LOGGER_FILE_FIELD_NAME, self.file).Msgf(format, i...)
}
