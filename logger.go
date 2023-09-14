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
	_LOGGER_DEFAULT_SKIP_FRAME_COUNT = 1
	_LOGGER_LEVEL_FIELD_NAME         = "level"
	_LOGGER_MESSAGE_FIELD_NAME       = "message"
	_LOGGER_APP_FIELD_NAME           = "app"
	_LOGGER_TIMESTAMP_FIELD_NAME     = "timestamp"
	_LOGGER_TIMESTAMP_FIELD_FORMAT   = zerolog.TimeFormatUnix
	_LOGGER_CALLER_FIELD_NAME        = "caller"
	_LOGGER_WRITER_SIZE              = 1000
	_LOGGER_POLL_INTERVAL            = 10 * time.Millisecond
	_LOGGER_FLUSH_DELAY              = _LOGGER_POLL_INTERVAL * 10
)

var _KlevelToZlevel = map[Level]zerolog.Level{
	LvlTrace: zerolog.TraceLevel,
	LvlDebug: zerolog.DebugLevel,
	LvlInfo:  zerolog.InfoLevel,
	LvlWarn:  zerolog.WarnLevel,
	LvlError: zerolog.ErrorLevel,
	LvlNone:  zerolog.Disabled,
}

type LoggerConfig struct {
	AppName        string
	Level          Level
	SkipFrameCount *int
}

type Logger struct {
	logger         *zerolog.Logger
	config         LoggerConfig
	out            io.Writer
	prefix         string
	header         string
	level          Level
	verbose        bool
	skipFrameCount int
}

func NewLogger(config LoggerConfig) *Logger {
	zerolog.LevelFieldName = _LOGGER_LEVEL_FIELD_NAME
	zerolog.MessageFieldName = _LOGGER_MESSAGE_FIELD_NAME
	zerolog.TimestampFieldName = _LOGGER_TIMESTAMP_FIELD_NAME
	zerolog.TimeFieldFormat = _LOGGER_TIMESTAMP_FIELD_FORMAT
	zerolog.CallerFieldName = _LOGGER_CALLER_FIELD_NAME

	if config.SkipFrameCount == nil {
		config.SkipFrameCount = ptr(_LOGGER_DEFAULT_SKIP_FRAME_COUNT)
	}

	_, file, line, _ := runtime.Caller(0)

	out := diode.NewWriter(os.Stdout, _LOGGER_WRITER_SIZE, _LOGGER_POLL_INTERVAL, func(missed int) {
		fmt.Fprintf(os.Stdout,
			"{\"%s\":\"%s\",\"%s\":\"%s\",\"%s\":\"%s:%d\",\"%s\":%d,\"%s\":\"Logger dropped %d messages\"}\n",
			zerolog.LevelFieldName, zerolog.ErrorLevel, _LOGGER_APP_FIELD_NAME,
			config.AppName, zerolog.CallerFieldName, file, line, zerolog.TimestampFieldName,
			time.Now().Unix(), zerolog.MessageFieldName, missed)
	})

	// Do not use Caller hook as runtime.Caller makes the logger up to 2.6x slower
	logger := zerolog.New(out).With().
		Str(_LOGGER_APP_FIELD_NAME, config.AppName).
		Timestamp().
		Logger().
		Level(_KlevelToZlevel[config.Level])

	return &Logger{
		logger:         &logger,
		config:         config,
		level:          config.Level,
		out:            &out,
		prefix:         config.AppName,
		header:         "",
		verbose:        LvlDebug >= config.Level,
		skipFrameCount: *config.SkipFrameCount,
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
		self.Info("Closing logger")

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

		self.Info("Closed logger")

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

func (self Logger) Level() Level { // nolint
	return self.level
}

func (self *Logger) SetLevel(l Level) {
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

func (self Logger) Print(i ...any) {
	self.logger.Log().Msg(fmt.Sprint(i...))
}

func (self Logger) Printf(format string, i ...any) {
	self.logger.Log().Msgf(format, i...)
}

func (self Logger) Debug(i ...any) {
	self.logger.Debug().Msg(fmt.Sprint(i...))
}

func (self Logger) Debugf(format string, i ...any) {
	self.logger.Debug().Msgf(format, i...)
}

func (self Logger) Info(i ...any) {
	self.logger.Info().Msg(fmt.Sprint(i...))
}

func (self Logger) Infof(format string, i ...any) {
	self.logger.Info().Msgf(format, i...)
}

func (self Logger) Warn(i ...any) {
	self.logger.Warn().Caller(self.skipFrameCount).Msg(fmt.Sprint(i...))
}

func (self Logger) Warnf(format string, i ...any) {
	self.logger.Warn().Caller(self.skipFrameCount).Msgf(format, i...)
}

func (self Logger) printDebugError(i ...any) {
	if len(i) >= 1 {
		switch err := i[0].(type) {
		case *Error:
			fmt.Printf("\x1b[91m%+v\x1b[0m\n", err.Unwrap()) // nolint
			i = i[1:]
		case *Exception:
			fmt.Printf("\x1b[91m%+v\x1b[0m\n", err.Unwrap()) // nolint
			i = i[1:]
		}
	}

	if len(i) == 0 {
		return
	}

	fmt.Printf("\x1b[91m%s\x1b[0m\n", fmt.Sprint(i...)) // nolint
}

func (self Logger) Error(i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(i...)
	} else {
		self.logger.Error().Caller(self.skipFrameCount).Msg(fmt.Sprint(i...))
	}
}

func (self Logger) Errorf(format string, i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(fmt.Sprintf(format, i...))
	} else {
		self.logger.Error().Caller(self.skipFrameCount).Msgf(format, i...)
	}
}

func (self Logger) Fatal(i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(i...)
		os.Exit(1) // nolint
	} else { // nolint
		self.logger.Fatal().Caller(self.skipFrameCount).Msg(fmt.Sprint(i...))
	}
}

func (self Logger) Fatalf(format string, i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(fmt.Sprintf(format, i...))
		os.Exit(1) // nolint
	} else { // nolint
		self.logger.Fatal().Caller(self.skipFrameCount).Msgf(format, i...)
	}
}

func (self Logger) Panic(i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(i...)
		panic(fmt.Sprint(i...))
	} else {
		self.logger.Panic().Caller(self.skipFrameCount).Msg(fmt.Sprint(i...))
	}
}

func (self Logger) Panicf(format string, i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(fmt.Sprintf(format, i...))
		panic(fmt.Sprintf(format, i...))
	} else {
		self.logger.Panic().Caller(self.skipFrameCount).Msgf(format, i...)
	}
}

func (self Logger) WithLevel(level Level, i ...any) {
	switch level {
	case LvlTrace:
		self.Print(i...)
	case LvlDebug:
		self.Debug(i...)
	case LvlInfo:
		self.Info(i...)
	case LvlWarn:
		self.Warn(i...)
	case LvlError:
		self.Error(i...)
	}
}

func (self Logger) WithLevelf(level Level, format string, i ...any) {
	switch level {
	case LvlTrace:
		self.Printf(format, i...)
	case LvlDebug:
		self.Debugf(format, i...)
	case LvlInfo:
		self.Infof(format, i...)
	case LvlWarn:
		self.Warnf(format, i...)
	case LvlError:
		self.Errorf(format, i...)
	}
}
