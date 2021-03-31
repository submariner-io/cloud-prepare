package aws

import "fmt"

type notFoundError struct {
	s string
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.s)
}

func newNotFoundError(msg string, args ...interface{}) error {
	return &notFoundError{fmt.Sprintf(msg, args...)}
}
