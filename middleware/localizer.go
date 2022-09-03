package middleware

import (
	"github.com/labstack/echo/v4"

	"golang.org/x/text/language"

	"github.com/neoxelox/kit"
)

const (
	_LOCALIZER_MIDDLEWARE_REQUEST_ACCEPT_LANGUAGE_HEADER = "Accept-Language"
)

type LocalizerConfig struct {
}

type Localizer struct {
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

		locales, _, err := language.ParseAcceptLanguage(
			request.Header.Get(_LOCALIZER_MIDDLEWARE_REQUEST_ACCEPT_LANGUAGE_HEADER))
		if err != nil {
			self.observer.Error(request.Context(), kit.ErrLocalizerGeneric().Wrap(err))
		}

		if len(locales) > 0 {
			ctx.SetRequest(request.WithContext(self.localizer.SetLocale(request.Context(), locales[0])))
		}

		return next(ctx)
	}
}
