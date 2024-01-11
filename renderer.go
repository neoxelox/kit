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

	"github.com/neoxelox/kit/util"
)

var (
	_RENDERER_DEFAULT_CONFIG = RendererConfig{
		TemplatesPath:       util.Pointer("./templates"),
		TemplateFilePattern: util.Pointer(`^.*\.(html|txt|md)$`),
	}
)

type RendererConfig struct {
	TemplatesPath       *string
	TemplateFilePattern *string
}

type Renderer struct {
	config   RendererConfig
	observer Observer
	renderer *template.Template
}

func NewRenderer(observer Observer, config RendererConfig) (*Renderer, error) {
	util.Merge(&config, _RENDERER_DEFAULT_CONFIG)

	*config.TemplatesPath = filepath.Clean(*config.TemplatesPath)

	extensions := regexp.MustCompile(*config.TemplateFilePattern)

	renderer := template.New("")

	err := filepath.WalkDir(*config.TemplatesPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return ErrRendererGeneric().WrapAs(err)
		}

		if info.IsDir() {
			return nil
		}

		if !extensions.MatchString(info.Name()) {
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

func (self *Renderer) Render(w io.Writer, name string, data any, c echo.Context) error { // nolint
	err := self.renderer.ExecuteTemplate(w, name, data)
	if err != nil {
		return ErrRendererGeneric().Wrap(err)
	}

	return nil
}

func (self *Renderer) RenderWriter(w io.Writer, template string, data any) error { // nolint
	err := self.renderer.ExecuteTemplate(w, template, data)
	if err != nil {
		return ErrRendererGeneric().Wrap(err)
	}

	return nil
}

func (self *Renderer) RenderBytes(template string, data any) ([]byte, error) { // nolint
	var w bytes.Buffer

	err := self.RenderWriter(&w, template, data)
	if err != nil {
		return nil, ErrRendererGeneric().Wrap(err)
	}

	return w.Bytes(), nil
}

func (self *Renderer) RenderString(template string, data any) (string, error) { // nolint
	bytes, err := self.RenderBytes(template, data) // nolint
	if err != nil {
		return "", ErrRendererGeneric().Wrap(err)
	}

	return string(bytes), nil
}
