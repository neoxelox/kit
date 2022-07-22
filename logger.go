package kit

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v4"
	gommon "github.com/labstack/gommon/log"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
)

const (
	_LOGGER_APP_FIELD_NAME = "app"
	_LOGGER_WRITER_SIZE    = 1000
	_LOGGER_POLL_INTERVAL  = 10 * time.Millisecond
)

type LoggerConfig struct {
	Environment     string
	App             string
	TimeFieldFormat *string
	Level           *zerolog.Level
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
	timeFieldFormat := zerolog.TimeFormatUnixMicro
	if config.TimeFieldFormat != nil {
		timeFieldFormat = *config.TimeFieldFormat
	}

	zerolog.TimeFieldFormat = timeFieldFormat
	zerolog.TimestampFieldName = "timestamp"
	zerolog.CallerSkipFrameCount = 3

	level := zerolog.DebugLevel
	if config.Environment != Environments.Development {
		level = zerolog.InfoLevel
	}

	if config.Level != nil {
		level = *config.Level
	}

	out := diode.NewWriter(os.Stdout, _LOGGER_WRITER_SIZE, _LOGGER_POLL_INTERVAL, func(missed int) {
		fmt.Fprintf(os.Stdout, "{\"%s\":\"%s\",\"%s\":\"Logger dropped %d messages\"}\n", _LOGGER_APP_FIELD_NAME,
			config.App, zerolog.MessageFieldName, missed)
	})

	return &Logger{
		logger:  zerolog.New(out).With().Str(_LOGGER_APP_FIELD_NAME, config.App).Timestamp().Logger().Level(level),
		config:  config,
		level:   level,
		out:     out,
		prefix:  config.App,
		header:  "",
		verbose: level == zerolog.DebugLevel,
	}
}

func (self Logger) Logger() *zerolog.Logger {
	return &self.logger
}

func (self *Logger) SetLogger(l zerolog.Logger) {
	self.logger = l
}

func (self Logger) Flush() {
	os.Stdout.Sync()
	os.Stderr.Sync()
}

func (self Logger) Close(ctx context.Context) error { // nolint
	self.logger.Info().Msg("Closing logger")

	self.Flush()

	writer, ok := self.out.(diode.Writer)
	if !ok {
		panic("logger writer is not diode")
	}

	return writer.Close() // nolint
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

	if i == nil || len(i) < 1 || i[0] == nil {
		return
	}

	for _, ie := range i {
		switch err := ie.(type) {
		case *Error:
			fmt.Printf("%+v\n", err.Unwrap()) // nolint
		case *Exception:
			fmt.Printf("%+v\n", err.Unwrap()) // nolint
		default:
			fmt.Printf("%+v\n", ie) // nolint
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
