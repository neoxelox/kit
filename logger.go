package kit

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/neoxelox/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"

	"github.com/neoxelox/kit/util"
)

const (
	_LOGGER_LEVEL_FIELD_NAME       = "level"
	_LOGGER_MESSAGE_FIELD_NAME     = "message"
	_LOGGER_SERVICE_FIELD_NAME     = "service"
	_LOGGER_TIMESTAMP_FIELD_NAME   = "timestamp"
	_LOGGER_TIMESTAMP_FIELD_FORMAT = zerolog.TimeFormatUnix
	_LOGGER_CALLER_FIELD_NAME      = "caller"
	_LOGGER_WRITER_SIZE            = 1000
	_LOGGER_POLL_INTERVAL          = 10 * time.Millisecond
	_LOGGER_FLUSH_DELAY            = _LOGGER_POLL_INTERVAL * 10
)

var (
	ErrLoggerGeneric  = errors.New("logger failed")
	ErrLoggerTimedOut = errors.New("logger timed out")
)

var _KlevelToZlevel = map[Level]zerolog.Level{
	LvlTrace: zerolog.TraceLevel,
	LvlDebug: zerolog.DebugLevel,
	LvlInfo:  zerolog.InfoLevel,
	LvlWarn:  zerolog.WarnLevel,
	LvlError: zerolog.ErrorLevel,
	LvlNone:  zerolog.Disabled,
}

var (
	_LOGGER_DEFAULT_CONFIG = LoggerConfig{
		SkipFrameCount: util.Pointer(1),
	}
)

type Level int

var (
	LvlTrace Level = -5
	LvlDebug Level = -4
	LvlInfo  Level = -3
	LvlWarn  Level = -2
	LvlError Level = -1
	LvlNone  Level = 0
)

type LoggerConfig struct {
	Level          Level
	Service        string
	SkipFrameCount *int
}

type Logger struct {
	config         LoggerConfig
	logger         *zerolog.Logger
	out            io.Writer
	prefix         string
	header         string
	level          Level
	verbose        bool
	skipFrameCount int
}

func NewLogger(config LoggerConfig) *Logger {
	util.Merge(&config, _LOGGER_DEFAULT_CONFIG)

	zerolog.LevelFieldName = _LOGGER_LEVEL_FIELD_NAME
	zerolog.MessageFieldName = _LOGGER_MESSAGE_FIELD_NAME
	zerolog.TimestampFieldName = _LOGGER_TIMESTAMP_FIELD_NAME
	zerolog.TimeFieldFormat = _LOGGER_TIMESTAMP_FIELD_FORMAT
	zerolog.CallerFieldName = _LOGGER_CALLER_FIELD_NAME

	_, file, line, _ := runtime.Caller(0)

	out := diode.NewWriter(os.Stdout, _LOGGER_WRITER_SIZE, _LOGGER_POLL_INTERVAL, func(missed int) {
		fmt.Fprintf(os.Stdout,
			"{\"%s\":\"%s\",\"%s\":\"%s\",\"%s\":\"%s:%d\",\"%s\":%d,\"%s\":\"Logger dropped %d messages\"}\n",
			zerolog.LevelFieldName, zerolog.ErrorLevel, _LOGGER_SERVICE_FIELD_NAME,
			config.Service, zerolog.CallerFieldName, file, line, zerolog.TimestampFieldName,
			time.Now().Unix(), zerolog.MessageFieldName, missed)
	})

	// Do not use Caller hook as runtime.Caller makes the logger up to 2.6x slower
	logger := zerolog.New(out).With().
		Str(_LOGGER_SERVICE_FIELD_NAME, config.Service).
		Timestamp().
		Logger().
		Level(_KlevelToZlevel[config.Level])

	return &Logger{
		logger:         &logger,
		config:         config,
		level:          config.Level,
		out:            &out,
		prefix:         config.Service,
		header:         "",
		verbose:        LvlDebug >= config.Level,
		skipFrameCount: *config.SkipFrameCount,
	}
}

func (self Logger) Logger() *zerolog.Logger {
	return self.logger
}

func (self Logger) Flush(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		// Wait for last minute logs
		time.Sleep(_LOGGER_FLUSH_DELAY)
		os.Stdout.Sync()
		os.Stderr.Sync()

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrLoggerTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}

func (self Logger) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		self.Info("Closing logger")

		err := self.Flush(ctx)
		if err != nil {
			return err
		}

		writer, ok := self.out.(*diode.Writer)
		if !ok {
			return ErrLoggerGeneric.Raise().With("logger writer %T is not diode", writer)
		}

		err = writer.Close()
		if err != nil {
			return ErrLoggerGeneric.Raise().Cause(err)
		}

		self.Info("Closed logger")

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrLoggerTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
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

func (self Logger) Level() Level {
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
	msg := ""
	for j, v := range i {
		if j > 0 {
			msg += " "
		}

		if s, ok := v.(fmt.Stringer); ok {
			msg += s.String()
		} else {
			msg += fmt.Sprintf("%v", v)
		}
	}
	self.logger.Log().Msg(msg)
}

func (self Logger) Printf(format string, i ...any) {
	self.logger.Log().Msgf(format, i...)
}

func (self Logger) Debug(i ...any) {
	msg := ""
	for j, v := range i {
		if j > 0 {
			msg += " "
		}

		if s, ok := v.(fmt.Stringer); ok {
			msg += s.String()
		} else {
			msg += fmt.Sprintf("%v", v)
		}
	}
	self.logger.Debug().Msg(msg)
}

func (self Logger) Debugf(format string, i ...any) {
	self.logger.Debug().Msgf(format, i...)
}

func (self Logger) Info(i ...any) {
	msg := ""
	for j, v := range i {
		if j > 0 {
			msg += " "
		}

		if s, ok := v.(fmt.Stringer); ok {
			msg += s.String()
		} else {
			msg += fmt.Sprintf("%v", v)
		}
	}
	self.logger.Info().Msg(msg)
}

func (self Logger) Infof(format string, i ...any) {
	self.logger.Info().Msgf(format, i...)
}

func (self Logger) Warn(i ...any) {
	msg := ""
	for j, v := range i {
		if j > 0 {
			msg += " "
		}

		if s, ok := v.(fmt.Stringer); ok {
			msg += s.String()
		} else {
			msg += fmt.Sprintf("%v", v)
		}
	}
	self.logger.Warn().Caller(self.skipFrameCount).Msg(msg)
}

func (self Logger) Warnf(format string, i ...any) {
	self.logger.Warn().Caller(self.skipFrameCount).Msgf(format, i...)
}

// nolint:forbidigo
func (self Logger) printDebugError(i ...any) {
	if len(i) >= 1 {
		switch err := i[0].(type) {
		case errors.Error:
			fmt.Printf("%+v", err)
			i = i[1:]
		case *errors.Error:
			fmt.Printf("%+v", err)
			i = i[1:]
		case HTTPError:
			fmt.Printf("%+v", err)
			i = i[1:]
		case *HTTPError:
			fmt.Printf("%+v", err)
			i = i[1:]
		}
	}

	if len(i) == 0 {
		return
	}

	fmt.Printf("\x1b[1;91m%s\x1b[0m\n", fmt.Sprint(i...))
}

func (self Logger) Error(i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(i...)
	} else {
		msg := ""
		for j, v := range i {
			if j > 0 {
				msg += " "
			}

			if s, ok := v.(fmt.Stringer); ok {
				msg += s.String()
			} else {
				msg += fmt.Sprintf("%v", v)
			}
		}
		self.logger.Error().Caller(self.skipFrameCount).Msg(msg)
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
		// Allow fast exitting only on debug level
		os.Exit(1) // nolint:revive
	} else { // nolint:revive
		msg := ""
		for j, v := range i {
			if j > 0 {
				msg += " "
			}

			if s, ok := v.(fmt.Stringer); ok {
				msg += s.String()
			} else {
				msg += fmt.Sprintf("%v", v)
			}
		}
		self.logger.Fatal().Caller(self.skipFrameCount).Msg(msg)
	}
}

func (self Logger) Fatalf(format string, i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(fmt.Sprintf(format, i...))
		// Allow fast exitting only on debug level
		os.Exit(1) // nolint:revive
	} else { // nolint:revive
		self.logger.Fatal().Caller(self.skipFrameCount).Msgf(format, i...)
	}
}

func (self Logger) Panic(i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(i...)
		// Allow panicking only on debug level
		panic(fmt.Sprint(i...))
	} else { // nolint:revive
		msg := ""
		for j, v := range i {
			if j > 0 {
				msg += " "
			}

			if s, ok := v.(fmt.Stringer); ok {
				msg += s.String()
			} else {
				msg += fmt.Sprintf("%v", v)
			}
		}
		self.logger.Panic().Caller(self.skipFrameCount).Msg(msg)
	}
}

func (self Logger) Panicf(format string, i ...any) {
	if LvlDebug >= self.level {
		self.printDebugError(fmt.Sprintf(format, i...))
		// Allow panicking only on debug level
		panic(fmt.Sprintf(format, i...))
	} else { // nolint:revive
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
