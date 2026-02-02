// Package env provides environment variable access and management.
package env

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

// System identifies the env system in the event bus.
const System event.System = "env"

// Event kinds for storage operations.
const (
	StorageRegister event.Kind = "storage.register"
	StorageDelete   event.Kind = "storage.delete"
	StorageUpdate   event.Kind = "storage.update"
)

// Event kinds for variable operations.
const (
	VariableRegister event.Kind = "variable.register"
	VariableDelete   event.Kind = "variable.delete"
	VariableUpdate   event.Kind = "variable.update"
)

// Event kinds for acceptance.
const (
	EnvAccept event.Kind = "accept"
	EnvReject event.Kind = "reject"
)

type (
	// Variable represents an environment variable with optional default value and access control.
	Variable struct {
		Meta         attrs.Bag   `json:"meta,omitempty"`
		ID           registry.ID `json:"id"`
		StorageID    registry.ID `json:"storage"`
		Name         string      `json:"variable"`
		DefaultValue string      `json:"default,omitempty"`
		ReadOnly     bool        `json:"readonly,omitempty"`
	}

	// Storage provides the interface for environment variable storage backends.
	Storage interface {
		Get(ctx context.Context, name string) (string, error)
		Set(ctx context.Context, name, value string) error
		Delete(ctx context.Context, name string) error
		List(ctx context.Context) (map[string]string, error)
	}

	// Registry provides the interface for managing environment variables across registered storages.
	Registry interface {
		// Get returns the value of the variable, or its default if not found in storage.
		Get(ctx context.Context, name string) (string, error)

		// Lookup returns the value and whether it was found in storage.
		Lookup(ctx context.Context, name string) (value string, found bool, err error)

		// Set stores the value for the variable.
		Set(ctx context.Context, name string, value string) error

		// All returns all variables from all storages.
		All(ctx context.Context) (map[string]string, error)

		// GetStorage retrieves a storage backend by its ID.
		GetStorage(ctx context.Context, id registry.ID) (Storage, error)

		// RegisterStorage registers a storage backend directly (synchronous).
		RegisterStorage(id registry.ID, storage Storage)
	}
)

// Validate checks if the variable configuration is valid.
func (v *Variable) Validate() error {
	for _, c := range v.Name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return apierror.New(apierror.Invalid, "invalid environment variable name: must only contain alphanumeric characters (a-z, A-Z, 0-9) and underscores").
				WithRetryable(apierror.False).
				WithDetails(attrs.NewBagFrom(map[string]any{"variable": v.Name, "reason": "must only contain alphanumeric characters (a-z, A-Z, 0-9) and underscores"}))
		}
	}
	if v.StorageID.NS == "" || v.StorageID.Name == "" {
		return ErrInvalidStorageID
	}
	return nil
}
