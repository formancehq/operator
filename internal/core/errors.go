package core

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
)

type ApplicationError struct {
	message      string
	requeueAfter time.Duration
}

func (e *ApplicationError) Error() string {
	return e.message
}

func (e *ApplicationError) Is(err error) bool {
	_, ok := err.(*ApplicationError)
	return ok
}

func (e *ApplicationError) WithMessage(msg string, args ...any) *ApplicationError {
	e.message = fmt.Sprintf(msg, args...)
	return e
}

func (e *ApplicationError) WithRequeueAfter(delay time.Duration) *ApplicationError {
	e.requeueAfter = delay
	return e
}

func NewApplicationError() *ApplicationError {
	return &ApplicationError{}
}

func NewStackNotFoundError() *ApplicationError {
	return NewApplicationError().WithMessage("stack not found")
}

func NewPendingError() *ApplicationError {
	return NewApplicationError().WithMessage("pending")
}

func NewMissingSettingsError(msg string) *ApplicationError {
	return NewApplicationError().WithMessage("%s", msg)
}

func IsApplicationError(err error) bool {
	return errors.Is(err, &ApplicationError{})
}

func ApplicationErrorRequeueAfter(err error) time.Duration {
	applicationError := &ApplicationError{}
	if errors.As(err, &applicationError) {
		return applicationError.requeueAfter
	}
	return 0
}
