package store

import (
	"errors"
	"fmt"
)

type StoreErrorType string

const (
	StoreErrorNotValidated     StoreErrorType = "not_validated"
	StoreErrorFailedToUpload   StoreErrorType = "failed_to_upload"
	StoreErrorFailedToDownload StoreErrorType = "failed_to_download"
)

type StoreError struct {
	Err error
}

/*
NewStoreError builds a StoreError for the given StoreErrorType.

When cause is non-nil, StoreError.Err is fmt.Errorf("%s: %w", string(errType), cause)
so the original failure stays in the chain (errors.Unwrap / errors.Is), e.g. for
AWS SDK error types. When cause is nil, Err is errors.New(string(errType)).
*/
func NewStoreError(errType StoreErrorType, cause error) *StoreError {
	if cause != nil {
		return &StoreError{
			Err: fmt.Errorf("%s: %w", string(errType), cause),
		}
	}

	return &StoreError{
		Err: errors.New(string(errType)),
	}
}

func (err *StoreError) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}

	return "unknown store error"
}

func (err *StoreError) Unwrap() error {
	return err.Err
}
