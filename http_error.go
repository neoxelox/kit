package kit

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/neoxelox/errors"
)

type HTTPError struct {
	cause  error
	code   string
	status int
}

func NewHTTPError(code string, status int) HTTPError {
	return HTTPError{
		cause:  nil,
		code:   code,
		status: status,
	}
}

func (self HTTPError) Cause(err error) *HTTPError {
	return &HTTPError{
		cause:  err,
		code:   self.code,
		status: self.status,
	}
}

func (self HTTPError) Unwrap() error {
	return self.cause
}

func (self HTTPError) Code() string {
	return self.code
}

func (self HTTPError) Status() int {
	return self.status
}

func (self *HTTPError) Redact() {
	self.cause = nil
}

func (self HTTPError) Is(err error) bool {
	if err == nil {
		return false
	}

	switch other := err.(type) {
	case HTTPError:
		return self.code == other.code && self.status == other.status
	case *HTTPError:
		return self.code == other.code && self.status == other.status
	}

	return false
}

func (self HTTPError) Has(err error) bool {
	if self.Is(err) {
		return true
	}

	if self.cause != nil {
		switch cause := self.cause.(type) {
		case errors.Error:
			return cause.Has(err)
		case *errors.Error:
			return cause.Has(err)
		default:
			return err == cause || err.Error() == cause.Error()
		}
	}

	return false
}

func (self HTTPError) String() string {
	if self.cause != nil {
		return fmt.Sprintf("%s (%d): %s", self.code, self.status, self.cause.Error())
	}

	return fmt.Sprintf("%s (%d)", self.code, self.status)
}

func (self HTTPError) Error() string {
	return self.String()
}

func (self HTTPError) MarshalText() ([]byte, error) {
	if self.cause != nil {
		// nolint:nilerr
		return []byte(fmt.Sprintf("code=%s status=%d message=%s", self.code, self.status, self.cause.Error())), nil
	}

	return []byte(fmt.Sprintf("code=%s status=%d", self.code, self.status)), nil
}

type _HTTPError struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (self HTTPError) MarshalJSON() ([]byte, error) {
	if self.cause != nil {
		return json.Marshal(_HTTPError{
			Code:    self.code,
			Message: self.cause.Error(),
		})
	}

	return json.Marshal(_HTTPError{
		Code: self.code,
	})
}

func (self HTTPError) Format(format fmt.State, verb rune) {
	switch verb {
	case 's':
		format.Write([]byte(self.String()))
	case 'v':
		report := fmt.Sprintf(
			"HTTP error \x1b[1;91m%s\x1b[0m with status \x1b[1;91m%d\x1b[0m\n", self.code, self.status)

		if self.cause != nil {
			report = fmt.Sprintf(
				"HTTP error \x1b[1;91m%s\x1b[0m with status \x1b[1;91m%d\x1b[0m caused by the following error:\n\n",
				self.code, self.status)

			switch cause := self.cause.(type) {
			case errors.Error:
				if format.Flag('+') {
					report += cause.StringReport()
				} else {
					report += cause.StringReport(false)
				}
			case *errors.Error:
				if format.Flag('+') {
					report += cause.StringReport()
				} else {
					report += cause.StringReport(false)
				}
			default:
				report += "\x1b[0;31m" + cause.Error() + "\x1b[0m (" +
					strings.TrimPrefix(reflect.TypeOf(cause).String(), "*") + ")\n"
			}
		}

		format.Write([]byte(report))
	default:
		format.Write([]byte(self.String()))
	}
}
