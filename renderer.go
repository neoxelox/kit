package kit

import (
	"bytes"
	"html/template"
	"io"
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"regexp"

	"github.com/labstack/echo/v4"
)

var (
	_RENDERER_DEFAULT_TEMPLATES_PATH      = "./templates"
	_RENDERER_DEFAULT_TEMPLATE_EXTENSIONS = regexp.MustCompile(`^.*\.(html|txt|md)$`)
)

type RendererConfig struct {
	TemplatesPath      *string
	TemplateExtensions *regexp.Regexp
}

type Renderer struct {
	config   RendererConfig
	observer Observer
	renderer *template.Template
}

func NewRenderer(observer Observer, config RendererConfig) (*Renderer, error) {
	observer.Anchor()

	if config.TemplatesPath == nil {
		config.TemplatesPath = &_RENDERER_DEFAULT_TEMPLATES_PATH
	}

	if config.TemplateExtensions == nil {
		config.TemplateExtensions = _RENDERER_DEFAULT_TEMPLATE_EXTENSIONS
	}

	*config.TemplatesPath = filepath.Clean(*config.TemplatesPath)

	renderer := template.New("")

	err := filepath.WalkDir(*config.TemplatesPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return ErrRendererGeneric().WrapAs(err)
		}

		if info.IsDir() {
			return nil
		}

		if !config.TemplateExtensions.MatchString(info.Name()) {
			return nil
		}

		name := path[len(*config.TemplatesPath)+1:]

		file, err := ioutil.ReadFile(path)
		if err != nil {
			return ErrRendererGeneric().WrapAs(err)
		}

		_, err = renderer.New(name).Parse(string(file))
		if err != nil {
			return ErrRendererGeneric().WrapAs(err)
		}

		return nil
	})
	if err != nil {
		return nil, ErrRendererGeneric().Wrap(err)
	}

	return &Renderer{
		config:   config,
		observer: observer,
		renderer: renderer,
	}, nil
}

func (self *Renderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error { // nolint
	err := self.renderer.ExecuteTemplate(w, name, data)
	if err != nil {
		return ErrRendererGeneric().Wrap(err)
	}

	return nil
}

func (self *Renderer) RenderWriter(w io.Writer, template string, data interface{}) error { // nolint
	err := self.renderer.ExecuteTemplate(w, template, data)
	if err != nil {
		return ErrRendererGeneric().Wrap(err)
	}

	return nil
}

func (self *Renderer) RenderBytes(template string, data interface{}) ([]byte, error) { // nolint
	var w bytes.Buffer

	err := self.RenderWriter(&w, template, data)
	if err != nil {
		return nil, ErrRendererGeneric().Wrap(err)
	}

	return w.Bytes(), nil
}

func (self *Renderer) RenderString(template string, data interface{}) (string, error) { // nolint
	bytes, err := self.RenderBytes(template, data) // nolint
	if err != nil {
		return "", ErrRendererGeneric().Wrap(err)
	}

	return string(bytes), nil
}
