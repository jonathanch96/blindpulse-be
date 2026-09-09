package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type FieldError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type Error struct {
	Code    string
	Message string
	HTTP    int
	Fields  []FieldError
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.Err }

func New(code string) *Error {
	status, message, ok := Definition(code)
	if !ok {
		status, message, code = http.StatusInternalServerError, catalog["INTERNAL_ERROR"].Message, "INTERNAL_ERROR"
	}
	return &Error{Code: code, Message: message, HTTP: status, Fields: []FieldError{}}
}

func Newf(code, format string, a ...any) *Error {
	e := New(code)
	e.Message = fmt.Sprintf(format, a...)
	return e
}

func Wrap(err error, code string) *Error {
	e := New(code)
	e.Err = err
	return e
}

func WithFields(code string, fields []FieldError) *Error {
	e := New(code)
	e.Fields = append([]FieldError(nil), fields...)
	return e
}

func Is(err error, code string) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Code == code
}
