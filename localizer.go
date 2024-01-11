package kit

import (
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"github.com/neoxelox/kit/util"
)

// TODO: enhance localization with go-i18n, go-localize or spreak

var (
	_LOCALIZER_DEFAULT_CONFIG = LocalizerConfig{
		LocalesPath:            util.Pointer("./locales"),
		LocaleExtensionPattern: util.Pointer(`^.*\.(yml|yaml)$`),
	}
)

type LocalizerConfig struct {
	DefaultLocale          language.Tag
	LocalesPath            *string
	LocaleExtensionPattern *string
}

type Localizer struct {
	config     LocalizerConfig
	observer   Observer
	copies     *map[language.Tag]map[string]string
	extensions *regexp.Regexp
}

func NewLocalizer(observer Observer, config LocalizerConfig) (*Localizer, error) {
	util.Merge(&config, _LOCALIZER_DEFAULT_CONFIG)

	*config.LocalesPath = filepath.Clean(*config.LocalesPath)

	extensions := regexp.MustCompile(*config.LocaleExtensionPattern)

	copiesByLang, err := _getCopies(&observer, *config.LocalesPath, extensions)
	if err != nil {
		return nil, ErrLocalizerGeneric().Wrap(err)
	}

	return &Localizer{
		config:     config,
		observer:   observer,
		copies:     copiesByLang,
		extensions: extensions,
	}, nil
}

func _getCopies(
	observer *Observer, localesPath string,
	localeExtensions *regexp.Regexp) (*map[language.Tag]map[string]string, error) {
	copiesByLang := make(map[language.Tag]map[string]string)

	err := filepath.WalkDir(localesPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return ErrLocalizerGeneric().WrapAs(err)
		}

		if info.IsDir() {
			return nil
		}

		if !localeExtensions.MatchString(info.Name()) {
			return nil
		}

		lang, err := language.Parse(info.Name()[:len(info.Name())-len(filepath.Ext(info.Name()))])
		if err != nil {
			return nil // nolint
		}

		file, err := ioutil.ReadFile(path)
		if err != nil {
			return ErrLocalizerGeneric().WrapAs(err)
		}

		copies := make(map[string]string)

		err = yaml.Unmarshal(file, &copies)
		if err != nil {
			return ErrLocalizerGeneric().WrapAs(err)
		}

		copiesByLang[lang] = copies

		return nil
	})
	if err != nil {
		return nil, ErrLocalizerGeneric().Wrap(err)
	}

	locales := len(copiesByLang)

	if locales < 1 {
		observer.Info(context.Background(), "No locales loaded")
		return &copiesByLang, nil
	}

	langs := make([]string, 0, locales)
	for k := range copiesByLang {
		langs = append(langs, k.String())
	}

	observer.Infof(context.Background(), "Loaded %d locales: %v", locales, strings.Join(langs, ", "))

	return &copiesByLang, nil
}

func (self *Localizer) Refresh() error {
	copiesByLang, err := _getCopies(&self.observer, *self.config.LocalesPath, self.extensions)
	if err != nil {
		return ErrLocalizerGeneric().Wrap(err)
	}

	self.copies = copiesByLang

	return nil
}

func (self Localizer) SetLocale(ctx context.Context, locale language.Tag) context.Context {
	return context.WithValue(ctx, KeyLocalizerLocale, locale)
}

func (self Localizer) GetLocale(ctx context.Context) language.Tag {
	if ctxLocale, ok := ctx.Value(KeyLocalizerLocale).(language.Tag); ok {
		return ctxLocale
	}

	return self.config.DefaultLocale
}

func (self Localizer) Localize(ctx context.Context, copy string, i ...any) string { // nolint
	copy = strings.ToUpper(copy) // nolint

	if trans, ok := (*self.copies)[self.GetLocale(ctx)][copy]; ok {
		return fmt.Sprintf(trans, i...)
	}

	if trans, ok := (*self.copies)[self.config.DefaultLocale][copy]; ok {
		return fmt.Sprintf(trans, i...)
	}

	return copy
}
