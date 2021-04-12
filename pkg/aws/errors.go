package aws

import (
	"fmt"

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

func isAWSError(err error, code string) bool {
	if awsErr, ok := err.(awserr.Error); ok {
		// Has to be checked as string, see https://github.com/aws/aws-sdk-go/issues/3235
		return awsErr.Code() == code
	}
	return false
}
