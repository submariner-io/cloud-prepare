package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
)

type notFoundError struct {
	s string
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.s)
}

func newNotFoundError(msg string, args ...interface{}) error {
	return &notFoundError{fmt.Sprintf(msg, args...)}
}

func isNotFoundError(err error) bool {
	_, ok := err.(*notFoundError)
	return ok
}

type compositeError struct {
	errs []error
}

func (e *compositeError) Error() string {
	var errStrings []string
	for _, err := range e.errs {
		fmt.Println()
		errStrings = append(errStrings, err.Error())
	}
	return fmt.Sprintf("Encountered %v errors: %s", len(e.errs), strings.Join(errStrings, "; "))
}

func newCompositeError(errs ...error) error {
	return &compositeError{errs: errs}
}

func appendIfError(errs []error, err error) []error {
	if err == nil {
		return errs
	}
	return append(errs, err)
}

func isAWSError(err error, code string) bool {
	if awsErr, ok := err.(awserr.Error); ok {
		// Has to be checked as string, see https://github.com/aws/aws-sdk-go/issues/3235
		return awsErr.Code() == code
	}
	return false
}
