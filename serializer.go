package kit

import (
	"github.com/labstack/echo/v4"
)

// TODO: faster serializer (ffjson or sonic)

type SerializerConfig struct{}

type Serializer struct {
	config     SerializerConfig
	serializer echo.DefaultJSONSerializer
	observer   Observer
}

func NewSerializer(observer Observer, config SerializerConfig) *Serializer {
	observer.Anchor()

	return &Serializer{
		config:     config,
		observer:   observer,
		serializer: echo.DefaultJSONSerializer{},
	}
}

func (self *Serializer) Serialize(c echo.Context, i interface{}, indent string) error {
	err := self.serializer.Serialize(c, i, indent)
	if err != nil {
		return Errors.ErrSerializerGeneric().Wrap(err)
	}

	return nil
}

func (self *Serializer) Deserialize(c echo.Context, i interface{}) error {
	err := self.serializer.Deserialize(c, i)
	if err != nil {
		return Errors.ErrSerializerGeneric().Wrap(err)
	}

	return nil
}
