package kit

import (
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/errors"
)

type Exception struct {
	inner      error
	identifier string
	code       string
	message    string
	status     int
}

func NewException(status int, code string) func() *Exception {
	return func() *Exception {
		return &Exception{
			inner:      errors.NewWithDepth(1, code),
			status:     status,
			identifier: code,
			code:       code,
			message:    code,
		}
	}
}

func (self *Exception) Cause(err error) *Exception {
	self.message = self.message + ": " + err.Error()
	self.inner = errors.WrapWithDepth(1, err, self.message)

	return self
}

func (self *Exception) CauseAs(err error) *Exception {
	if other, ok := err.(*Exception); ok {
		self.identifier = other.identifier
	} else {
		self.identifier = err.Error()
	}

	self.inner = errors.WrapWithDepth(1, err, self.message)

	return self
}

func (self *Exception) With(message string) *Exception {
	self.message = self.message + ": " + message
	self.inner = errors.NewWithDepth(1, self.message)

	return self
}

func (self *Exception) Withf(message string, args ...interface{}) *Exception {
	self.message = self.message + ": " + fmt.Sprintf(message, args...)
	self.inner = errors.NewWithDepth(1, self.message)

	return self
}

func (self *Exception) Redact() *Exception {
	self.message = ""

	return self
}

func (self Exception) Code() string {
	return self.code
}

func (self Exception) Status() int {
	return self.status
}

func (self Exception) Error() string {
	return self.inner.Error()
}

func (self Exception) Unwrap() error {
	return self.inner
}

func (self Exception) Is(err error) bool {
	if other, ok := err.(*Exception); ok {
		return self.identifier == other.identifier
	}

	return false
}

func (self Exception) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct { // nolint
		Code    string `json:"code"`
		Message string `json:"message,omitempty"`
	}{
		Code:    self.code,
		Message: self.message,
	})
}
