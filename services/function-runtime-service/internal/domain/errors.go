// Package domain holds the function-runtime-service sentinel errors
// and small pure helpers that don't fit cleanly under repo or
// executor.
package domain

import "errors"

// Sentinel errors. The HTTP layer maps these to specific status codes
// (see internal/handlers).
var (
	ErrNotFound             = errors.New("function: not found")
	ErrAlreadyExists        = errors.New("function: already exists")
	ErrInvalidArgument      = errors.New("function: invalid argument")
	ErrPreconditionFailed   = errors.New("function: precondition failed")
	ErrTenantMismatch       = errors.New("function: tenant mismatch")
	ErrVersionNotFound      = errors.New("function: version not found")
	ErrNoActiveVersion      = errors.New("function: no active version")
	ErrExecutorNotAvailable = errors.New("function: executor not available")
	ErrExecutionTimeout     = errors.New("function: execution timed out")
	ErrExecutionFailed      = errors.New("function: execution failed")
)
