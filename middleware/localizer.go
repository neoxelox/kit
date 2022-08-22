package middleware

import (
	"github.com/labstack/echo/v4"

	"golang.org/x/text/language"

	"github.com/neoxelox/kit"
)

type LocalizerConfig struct {
}

type Localizer struct {
	kit.Middleware
	config    LocalizerConfig
	observer  kit.Observer
	localizer kit.Localizer
}

func NewLocalizer(observer kit.Observer, localizer kit.Localizer, config LocalizerConfig) *Localizer {
	return &Localizer{
		config:    config,
		observer:  observer,
		localizer: localizer,
	}
}

func (self *Localizer) Handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		request := ctx.Request()

		locales, _, err := language.ParseAcceptLanguage(request.Header.Get("Accept-Language"))
		if err != nil { // nolint
			self.observer.Error(request.Context(), kit.ErrLocalizerGeneric().Wrap(err))
		} else if len(locales) < 1 {
			self.observer.Error(
				request.Context(), kit.ErrLocalizerGeneric().With("no locales found in Accept-Language header"))
		} else {
			ctx.SetRequest(request.WithContext(self.localizer.SetLocale(request.Context(), locales[0])))
		}

		return next(ctx)
	}
}
