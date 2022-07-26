package kit

import (
	"context"
	"encoding/json"
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/language"
)

// TODO: enhance localization with go-i18n, go-localize or spreak

const _LOCALIZER_LOCALE_CTX_KEY = _BASE_CTX_KEY + "localizer:locale"

var (
	_LOCALIZER_DEFAULT_LOCALES_PATH      = "./locales"
	_LOCALIZER_DEFAULT_LOCALE_EXTENSIONS = regexp.MustCompile(`^.*\.(json)$`)
)

type LocalizerConfig struct {
	LocalesPath      *string
	LocaleExtensions *regexp.Regexp
	DefaultLocale    language.Tag
}

type Localizer struct {
	config   LocalizerConfig
	observer Observer
	copies   map[language.Tag]map[string]string
}

func NewLocalizer(observer Observer, config LocalizerConfig) (*Localizer, error) {
	observer.Anchor()

	if config.LocalesPath == nil {
		config.LocalesPath = &_LOCALIZER_DEFAULT_LOCALES_PATH
	}

	config.LocaleExtensions = _LOCALIZER_DEFAULT_LOCALE_EXTENSIONS

	*config.LocalesPath = filepath.Clean(*config.LocalesPath)

	copiesByLang := make(map[language.Tag]map[string]string)

	err := filepath.WalkDir(*config.LocalesPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return Errors.ErrLocalizerGeneric().WrapAs(err)
		}

		if info.IsDir() {
			return nil
		}

		if !config.LocaleExtensions.MatchString(info.Name()) {
			return nil
		}

		lang, err := language.Parse(info.Name()[:len(info.Name())-len(filepath.Ext(info.Name()))])
		if err != nil {
			return nil
		}

		file, err := ioutil.ReadFile(path)
		if err != nil {
			return Errors.ErrLocalizerGeneric().WrapAs(err)
		}

		copies := make(map[string]string)

		err = json.Unmarshal(file, &copies)
		if err != nil {
			return Errors.ErrLocalizerGeneric().WrapAs(err)
		}

		copiesByLang[lang] = copies

		return nil
	})
	if err != nil {
		return nil, Errors.ErrLocalizerGeneric().Wrap(err)
	}

	return &Localizer{
		config:   config,
		observer: observer,
		copies:   copiesByLang,
	}, nil
}

func (self Localizer) SetLocale(ctx context.Context, locale language.Tag) context.Context {
	return context.WithValue(ctx, _LOCALIZER_LOCALE_CTX_KEY, locale)
}

func (self Localizer) GetLocale(ctx context.Context) language.Tag {
	if ctxLocale, ok := ctx.Value(_LOCALIZER_LOCALE_CTX_KEY).(language.Tag); ok {
		return ctxLocale
	}

	return self.config.DefaultLocale
}

func (self Localizer) Localize(ctx context.Context, copy string) string {
	copy = strings.ToUpper(copy)

	if trans, ok := self.copies[self.GetLocale(ctx)][copy]; ok {
		return trans
	}

	if trans, ok := self.copies[self.config.DefaultLocale][copy]; ok {
		return trans
	}

	return copy
}
