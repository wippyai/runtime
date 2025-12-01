package os

import (
	"context"
	stdos "os"
	"strings"

	"github.com/wippyai/runtime/api/env"
	enverr "github.com/wippyai/runtime/service/env"
)

// Storage provides read-only access to OS environment variables.
// Set and Delete operations return ErrStorageReadOnly.
type Storage struct{}

// Verify Storage implements env.Storage
var _ env.Storage = (*Storage)(nil)

// NewStorage creates a new OS environment storage.
func NewStorage() *Storage {
	return &Storage{}
}

// Get retrieves an environment variable by name.
// Returns ErrVariableNotFound if the variable is not set.
// Empty string is a valid value if the variable is set.
func (s *Storage) Get(_ context.Context, key string) (string, error) {
	val, exists := stdos.LookupEnv(key)
	if !exists {
		return "", env.ErrVariableNotFound
	}
	return val, nil
}

// Set is not supported for OS storage.
func (s *Storage) Set(_ context.Context, _, _ string) error {
	return enverr.ErrStorageReadOnly
}

// Delete is not supported for OS storage.
func (s *Storage) Delete(_ context.Context, _ string) error {
	return enverr.ErrStorageReadOnly
}

// List returns all OS environment variables.
func (s *Storage) List(_ context.Context) (map[string]string, error) {
	result := make(map[string]string)
	for _, envVar := range stdos.Environ() {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}
