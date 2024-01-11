package kit

import (
	"encoding/json"

	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit/util"
)

// TODO: faster serializer (ffjson or sonic)

var (
	_SERIALIZER_DEFAULT_CONFIG = SerializerConfig{}
)

type SerializerConfig struct {
}

type Serializer struct {
	config   SerializerConfig
	observer Observer
}

func NewSerializer(observer Observer, config SerializerConfig) *Serializer {
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
		return ErrSerializerGeneric().Wrap(err)
	}

	return nil
}

func (self *Serializer) Deserialize(c echo.Context, i any) error {
	decoder := json.NewDecoder(c.Request().Body)

	err := decoder.Decode(i)
	if err != nil {
		if ute, ok := err.(*json.UnmarshalTypeError); ok {
			return ErrSerializerGeneric().Withf("Unmarshal type error: expected=%v, got=%v, field=%v, offset=%v",
				ute.Type, ute.Value, ute.Field, ute.Offset).Wrap(err)
		}

		if se, ok := err.(*json.SyntaxError); ok {
			return ErrSerializerGeneric().Withf("Syntax error: offset=%v, error=%v", se.Offset, se.Error()).Wrap(err)
		}

		return ErrSerializerGeneric().Wrap(err)
	}

	return nil
}
