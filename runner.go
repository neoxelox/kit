package kit

import (
	"context"
	"fmt"
	"os"

	"github.com/mkideal/cli"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

const (
	_RUNNER_ROOT_COMMAND_DESCRIPTION    = "%s runner"
	_RUNNER_HELP_COMMAND_NAME           = "help"
	_RUNNER_HELP_COMMAND_DESCRIPTION    = "show command usage information"
	_RUNNER_VERSION_COMMAND_NAME        = "version"
	_RUNNER_VERSION_COMMAND_DESCRIPTION = "show runner version"
)

var (
	ErrRunnerGeneric  = errors.New("runner failed")
	ErrRunnerTimedOut = errors.New("runner timed out")
)

var (
	_RUNNER_DEFAULT_CONFIG = RunnerConfig{}
)

type RunnerConfig struct {
	Service string
	Release string
}

type RunnerHandler func(context.Context, *cli.Context) error

type Runner struct {
	config       RunnerConfig
	observer     *Observer
	runner       *cli.Command
	errorHandler *ErrorHandler
	middlewares  []func(RunnerHandler) RunnerHandler
}

func NewRunner(observer *Observer, errorHandler *ErrorHandler, config RunnerConfig) *Runner {
	util.Merge(&config, _RUNNER_DEFAULT_CONFIG)

	cli.SetUsageStyle(cli.NormalStyle)

	middlewares := make([]func(RunnerHandler) RunnerHandler, 0)

	runner := cli.Root(&cli.Command{
		Desc: fmt.Sprintf(_RUNNER_ROOT_COMMAND_DESCRIPTION, config.Service),
		Fn: func(ctx *cli.Context) error {
			ctx.WriteUsage()

			return nil
		},
	})

	runner.Register(&cli.Command{
		Name: _RUNNER_HELP_COMMAND_NAME,
		Desc: _RUNNER_HELP_COMMAND_DESCRIPTION,
		Fn:   cli.HelpCommandFn,
	})

	runner.Register(&cli.Command{
		Name: _RUNNER_VERSION_COMMAND_NAME,
		Desc: _RUNNER_VERSION_COMMAND_DESCRIPTION,
		Fn: func(ctx *cli.Context) error {
			ctx.String("release: %s\n", config.Release)

			return nil
		},
	})

	return &Runner{
		config:       config,
		observer:     observer,
		runner:       runner,
		errorHandler: errorHandler,
		middlewares:  middlewares,
	}
}

func (self *Runner) Run(ctx context.Context) error {
	self.observer.Infof(ctx, "Runner started with arguments %v", os.Args[1:])

	err := self.runner.Run(os.Args[1:])
	if err != nil && err != cli.ExitError {
		return ErrRunnerGeneric.Raise().Cause(err)
	}

	return nil
}

func (self *Runner) Use(middleware ...func(RunnerHandler) RunnerHandler) {
	self.middlewares = append(self.middlewares, middleware...)
}

func (self *Runner) Register(command string, handler RunnerHandler, args any, description ...string) {
	_description := util.Optional(description, "")

	self.runner.Register(&cli.Command{
		Name: command,
		Desc: _description,
		Argv: func() any { return util.Copy(args) },
		Fn: func(ctx *cli.Context) error {
			// Error handler has to be the last middleware in order to use the possible traced context
			handler = self.errorHandler.HandleCommand(handler)

			for i := len(self.middlewares) - 1; i >= 0; i-- {
				handler = self.middlewares[i](handler)
			}

			return handler(context.Background(), ctx)
		},
	})
}

func (self *Runner) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		// TODO: Close runner gracefully

		// Dummy log in order to mantain consistency although CLI runner has no close() method
		self.observer.Info(ctx, "Closing runner")
		self.observer.Info(ctx, "Closed runner")

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrRunnerTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}
