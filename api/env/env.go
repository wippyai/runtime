package env

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
)

const (
	System event.System = "env"

	// Storage events
	StorageRegister event.Kind = "storage.register"
	StorageDelete   event.Kind = "storage.delete"
	StorageUpdate   event.Kind = "storage.update"

	// Variable events
	VariableRegister event.Kind = "variable.register"
	VariableDelete   event.Kind = "variable.delete"
	VariableUpdate   event.Kind = "variable.update"
)

const (
	// Storage registry kinds
	KindStorageMemory registry.Kind = "env.storage.memory"
	KindStorageFile   registry.Kind = "env.storage.file"
	KindStorageOS     registry.Kind = "env.storage.os"

	// Variable registry kind
	KindVariable registry.Kind = "env.variable"
)

var (
	ErrVariableNotFound    = errors.New("environment variable not found")
	ErrStorageNotFound     = errors.New("environment storage backend not found")
	ErrVariableReadOnly    = errors.New("environment variable is read-only")
	ErrInvalidVariableName = errors.New("invalid environment variable name")
)

type (
	Storage interface {
		supervisor.Service
		resource.Provider

		Get(ctx context.Context, name string) (string, error)
		Set(ctx context.Context, name, value string) error
		Delete(ctx context.Context, name string) error
		List(ctx context.Context) (map[string]string, error)
	}

	Registry interface {
		Get(ctx context.Context, name string) (string, error)
		GetFromStorage(ctx context.Context, name string) (string, error)
		Set(ctx context.Context, name string, value string) error
		All(ctx context.Context) (map[string]string, error)
	}

	Variable struct {
		Meta         registry.Metadata `json:"meta,omitempty"`
		Name         string            `json:"name"`
		EnvName      string            `json:"variable,omitempty"`
		DefaultValue string            `json:"default,omitempty"`
		ReadOnly     bool              `json:"readonly,omitempty"`
		StorageID    string            `json:"storage"`
	}
)

func (v *Variable) Validate() error {
	if v.Name == "" {
		return errors.New("env variable name cannot be empty")
	}

	if v.EnvName != "" {
		for _, c := range v.EnvName {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
				return fmt.Errorf("env variable name must only contain alphanumeric characters (a-z, A-Z, 0-9) and underscores, but received %s", v.EnvName)
			}
		}
	} else {
		return fmt.Errorf("env variable env name cannot be empty")
	}

	if v.StorageID == "" {
		return errors.New("storage ID cannot be empty")
	}

	id := registry.ParseID(v.StorageID)
	if id.NS == "" || id.Name == "" {
		return errors.New("invalid storage ID format, must be 'namespace:name'")
	}

	return nil
}
