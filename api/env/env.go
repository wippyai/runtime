// Package env provides environment variable access and management.
package env

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

const (
	System event.System = "env"

	StorageRegister event.Kind = "storage.register"
	StorageDelete   event.Kind = "storage.delete"
	StorageUpdate   event.Kind = "storage.update"

	VariableRegister event.Kind = "variable.register"
	VariableDelete   event.Kind = "variable.delete"
	VariableUpdate   event.Kind = "variable.update"

	Accepted event.Kind = "accept"
	Rejected event.Kind = "reject"
)

// Variable represents an environment variable with optional default value and access control.
type Variable struct {
	ID           registry.ID       `json:"id"`
	Meta         registry.Metadata `json:"meta,omitempty"`
	Name         string            `json:"variable"`
	DefaultValue string            `json:"default,omitempty"`
	ReadOnly     bool              `json:"readonly,omitempty"`
	StorageID    registry.ID       `json:"storage"`
}

// Storage provides the interface for environment variable storage backends.
type Storage interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name, value string) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) (map[string]string, error)
}

// Registry provides the interface for managing environment variables across registered storages.
type Registry interface {
	// Get returns the value of the variable, or its default if not found in storage.
	// Returns error only for actual failures (storage not found, etc.).
	Get(ctx context.Context, name string) (string, error)

	// Lookup returns the value and whether it was found in storage.
	// Unlike Get, it does not fall back to default values.
	// Use this when you need to distinguish "not set" from "set to empty".
	Lookup(ctx context.Context, name string) (value string, found bool, err error)

	// Set stores the value for the variable.
	Set(ctx context.Context, name string, value string) error

	// All returns all variables from all storages.
	All(ctx context.Context) (map[string]string, error)
}

func (v *Variable) Validate() error {
	for _, c := range v.Name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return fmt.Errorf("env variable name must only contain alphanumeric characters (a-z, A-Z, 0-9) and underscores, but received %s", v.Name)
		}
	}

	if v.StorageID.NS == "" || v.StorageID.Name == "" {
		return errors.New("invalid storage ID format, must have both namespace and name")
	}

	return nil
}
