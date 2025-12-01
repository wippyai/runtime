package env

import (
	"errors"

	"github.com/wippyai/runtime/api/env"
)

// Re-export API errors for convenience
var (
	ErrVariableNotFound    = env.ErrVariableNotFound
	ErrStorageNotFound     = env.ErrStorageNotFound
	ErrVariableReadOnly    = env.ErrVariableReadOnly
	ErrInvalidVariableName = env.ErrInvalidVariableName
)

// Storage-specific errors
var (
	ErrStorageReadOnly    = errors.New("storage is read-only")
	ErrStorageClosed      = errors.New("storage is closed")
	ErrNoStorages         = errors.New("at least one storage must be provided")
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrUnsupportedKind    = errors.New("unsupported entry kind")
	ErrStorageExists      = errors.New("storage already exists")
	ErrStorageNotExists   = errors.New("storage does not exist")
	ErrDecodeConfig       = errors.New("failed to decode configuration")
	ErrCreateStorage      = errors.New("failed to create storage")
	ErrStorageNotWritable = errors.New("storage does not support write operations")
)
