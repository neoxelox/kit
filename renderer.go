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

const _RENDERER_DEFAULT_TEMPLATES_PATH = "./templates"

var _RENDERER_DEFAULT_TEMPLATE_EXTENSIONS = regexp.MustCompile(`^.*\.(html|txt|md)$`)

type RendererConfig struct {
	TemplatesPath      *string
	TemplateExtensions *regexp.Regexp
}

type Renderer struct {
	config   RendererConfig
	observer Observer
	renderer *template.Template
}

func NewRenderer(observer Observer, config RendererConfig) *Renderer {
	observer.Anchor()

	templatesPath := _RENDERER_DEFAULT_TEMPLATES_PATH
	if config.TemplatesPath != nil {
		templatesPath = *config.TemplatesPath
	}

	templateExtensions := _RENDERER_DEFAULT_TEMPLATE_EXTENSIONS
	if config.TemplateExtensions != nil {
		templateExtensions = config.TemplateExtensions
	}

	renderer := template.New("")
	templatesPath = filepath.Clean(templatesPath)

	err := filepath.WalkDir(templatesPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return Errors.ErrRendererGeneric().WrapAs(err)
		}

		if info.IsDir() {
			return nil
		}

		name := path[len(templatesPath)+1:]

		if !templateExtensions.MatchString(name) {
			return nil
		}

		file, err := ioutil.ReadFile(path)
		if err != nil {
			return Errors.ErrRendererGeneric().WrapAs(err)
		}

		_, err = renderer.New(name).Parse(string(file))
		if err != nil {
			return Errors.ErrRendererGeneric().WrapAs(err)
		}

		return nil
	})
	if err != nil {
		panic(Errors.ErrRendererGeneric().Wrap(err))
	}

	return &Renderer{
		config:   config,
		observer: observer,
		renderer: renderer,
	}
}

func (self *Renderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error { // nolint
	err := self.renderer.ExecuteTemplate(w, name, data)
	if err != nil {
		return Errors.ErrRendererGeneric().Wrap(err)
	}

	return nil
}

func (self *Renderer) RenderWriter(w io.Writer, template string, data interface{}) error { // nolint
	err := self.renderer.ExecuteTemplate(w, template, data)
	if err != nil {
		return Errors.ErrRendererGeneric().Wrap(err)
	}

	return nil
}

func (self *Renderer) RenderBytes(template string, data interface{}) ([]byte, error) { // nolint
	var w bytes.Buffer

	err := self.RenderWriter(&w, template, data)
	if err != nil {
		return nil, Errors.ErrRendererGeneric().Wrap(err)
	}

	return w.Bytes(), nil
}

func (self *Renderer) RenderString(template string, data interface{}) (string, error) { // nolint
	bytes, err := self.RenderBytes(template, data) // nolint
	if err != nil {
		return "", Errors.ErrRendererGeneric().Wrap(err)
	}

	return string(bytes), nil
}
