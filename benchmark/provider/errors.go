package provider

import "errors"

var (
	ErrProviderUnavailable  = errors.New("provider unavailable")
	ErrProviderExecution    = errors.New("provider execution error")
	ErrInvalidSessionConfig = errors.New("invalid session config")
	ErrInvalidQueryRequest  = errors.New("invalid query request")
	ErrProviderProtocol     = errors.New("provider protocol error")
)
