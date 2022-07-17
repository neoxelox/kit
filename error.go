package kit

import (
	"fmt"

	"github.com/cockroachdb/errors"
)

type Error struct {
	inner      error
	identifier string
	message    string
}

func NewError(message string) func() *Error {
	return func() *Error {
		return &Error{
			inner:      errors.NewWithDepth(1, message),
			identifier: message,
			message:    message,
		}
	}
}

func (self *Error) Wrap(err error) *Error {
	self.inner = errors.WrapWithDepth(1, err, self.message)

	return self
}

func (self *Error) WrapWithDepth(depth int, err error) *Error {
	self.inner = errors.WrapWithDepth(depth+1, err, self.message)

	return self
}

func (self *Error) WrapAs(err error) *Error {
	if other, ok := err.(*Error); ok {
		self.identifier = other.identifier
	} else {
		self.identifier = err.Error()
	}

	self.inner = errors.WrapWithDepth(1, err, self.message)

	return self
}

func (self *Error) WrapAsWithDepth(depth int, err error) *Error {
	if other, ok := err.(*Error); ok {
		self.identifier = other.identifier
	} else {
		self.identifier = err.Error()
	}

	self.inner = errors.WrapWithDepth(depth+1, err, self.message)

	return self
}

func (self *Error) With(message string) *Error {
	self.message = self.message + ": " + message
	self.inner = errors.NewWithDepth(1, self.message)

	return self
}

func (self *Error) Withf(message string, args ...interface{}) *Error {
	self.message = self.message + ": " + fmt.Sprintf(message, args...)
	self.inner = errors.NewWithDepth(1, self.message)

	return self
}

func (self Error) Error() string {
	return self.inner.Error()
}

func (self Error) Unwrap() error {
	return self.inner
}

func (self Error) Is(err error) bool {
	if other, ok := err.(*Error); ok {
		return self.identifier == other.identifier
	}

	return false
}
