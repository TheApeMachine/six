package store

import "errors"

type StoreErrorType string

const (
	StoreErrorNotValidated     StoreErrorType = "not_validated"
	StoreErrorFailedToUpload   StoreErrorType = "failed_to_upload"
	StoreErrorFailedToDownload StoreErrorType = "failed_to_download"
)

type StoreError struct {
	Err error
}

func NewStoreError(err StoreErrorType) *StoreError {
	return &StoreError{Err: errors.New(string(err))}
}

func (err *StoreError) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}

	return "value error"
}

func (err *StoreError) Unwrap() error {
	return err.Err
}
