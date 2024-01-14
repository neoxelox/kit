package kit

import (
	"encoding/json"

	"github.com/labstack/echo/v4"
	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

// TODO: faster serializer (ffjson or sonic)

var (
	ErrSerializerGeneric = errors.New("serializer failed")
)

var (
	_SERIALIZER_DEFAULT_CONFIG = SerializerConfig{}
)

type SerializerConfig struct {
}

type Serializer struct {
	config   SerializerConfig
	observer *Observer
}

func NewSerializer(observer *Observer, config SerializerConfig) *Serializer {
	util.Merge(&config, _SERIALIZER_DEFAULT_CONFIG)

	return &Serializer{
		config:   config,
		observer: observer,
	}
}

func (self *Serializer) Serialize(c echo.Context, i any, indent string) error {
	encoder := json.NewEncoder(c.Response())

	if indent != "" {
		encoder.SetIndent("", indent)
	}

	encoder.SetEscapeHTML(false)

	err := encoder.Encode(i)
	if err != nil {
		return ErrSerializerGeneric.Raise().Cause(err)
	}

	return nil
}

func (self *Serializer) Deserialize(c echo.Context, i any) error {
	decoder := json.NewDecoder(c.Request().Body)

	err := decoder.Decode(i)
	if err != nil {
		if ute, ok := err.(*json.UnmarshalTypeError); ok {
			return ErrSerializerGeneric.Raise().
				With("unmarshal type error").
				Extra(map[string]any{"field": ute.Field, "expected": ute.Type, "actual": ute.Value, "offset": ute.Offset}).
				Cause(ute)
		}

		if se, ok := err.(*json.SyntaxError); ok {
			return ErrSerializerGeneric.Raise().
				With("syntax error").
				Extra(map[string]any{"offset": se.Offset}).
				Cause(se)
		}

		return ErrSerializerGeneric.Raise().Cause(err)
	}

	return nil
}
