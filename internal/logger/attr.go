package logger

import (
	"strings"
)

// ErrAttr returns an error field for zerolog structured logging
func ErrAttr(err error) string {
	return "err"
}

// ErrValue returns an error message formatted for zerolog error field
func ErrValue(err error) string {
	return strings.TrimSpace(err.Error())
}

// ErrorsValue returns a formatted string array of error messages
func ErrorsValue(errors ...error) []string {
	var stringErrors []string

	for _, err := range errors {
		stringErrors = append(stringErrors, strings.TrimSpace(err.Error()))
	}

	return stringErrors
}
