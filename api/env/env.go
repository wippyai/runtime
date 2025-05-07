package env

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	// System identifies the environment system in the event bus
	System event.System = "env"

	// StorageRegister is sent TO env service to register
	StorageRegister event.Kind = "env.storage.register"
	// StorageDelete is sent TO env service to delete a storage backend
	StorageDelete event.Kind = "env.storage.delete"
)

// Registry kind constants for different SQL database types
const (
	// KindMemory identifies a Memory ENV store
	KindMemory registry.Kind = "env.memory"

	// KindFile identifies a File ENV store
	KindFile registry.Kind = "env.file"
)

// Common errors returned by the env package
var (
	ErrVariableNotFound    = errors.New("environment variable not found")
	ErrStorageNotFound     = errors.New("environment storage backend not found")
	ErrVariableReadOnly    = errors.New("environment variable is read-only")
	ErrInvalidVariableName = errors.New("invalid environment variable name")
)

type (
	// Storage defines the interface for environment variable storage backends
	Storage interface {
		// Get retrieves a variable's value
		Get(ctx context.Context, name string) (string, error)

		// Set stores a variable's value
		Set(ctx context.Context, name, value string) error

		// Delete removes a variable from storage
		Delete(ctx context.Context, name string) error

		// List returns all variable names and values in this storage
		List(ctx context.Context) (map[string]string, error)
	}

	// Registry defines the interface for accessing environment variables
	Registry interface {
		// Get retrieves an environment variable by name from a specific storage
		Get(ctx context.Context, name string) (string, error)

		// All returns all env storages
		All(ctx context.Context) ([]Storage, error)
	}

	// Variable represents a variable registration payload
	Variable struct {
		// Meta contains additional metadata
		Meta registry.Metadata `json:"meta,omitempty"`

		// Name is the variable's name
		Name string `json:"name"`

		// EnvName is the actual environment variable name if different from Name
		EnvName string `json:"variable,omitempty"`

		// DefaultValue is the default value if not set
		DefaultValue string `json:"default,omitempty"`

		// ReadOnly indicates if the variable cannot be modified at runtime
		ReadOnly bool `json:"readonly,omitempty"`

		// StorageID is the ID of the storage backend
		StorageID string `json:"storage"`
	}
)

// Validate checks if the Variable configuration is valid
func (v *Variable) Validate() error {
	// Validate Name (required)
	if v.Name == "" {
		return errors.New("env variable name cannot be empty")
	}

	// Validate EnvName if provided
	if v.EnvName != "" {
		// Validate that EnvName only contains alphanumeric characters and underscores
		for _, c := range v.EnvName {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
				return fmt.Errorf("env variable name must only contain alphanumeric characters (a-z, A-Z, 0-9) and underscores, but received %s", v.EnvName)
			}
		}
	} else {
		return fmt.Errorf("env variable env name cannot be empty")
	}

	// Validate StorageID (required)
	if v.StorageID == "" {
		return errors.New("storage ID cannot be empty")
	}

	// Check StorageID format
	id := registry.ParseID(v.StorageID)
	if id.NS == "" || id.Name == "" {
		return errors.New("invalid storage ID format, must be 'namespace:name'")
	}

	return nil
}
