package env

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
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

var (
	ErrVariableNotFound    = errors.New("environment variable not found")
	ErrStorageNotFound     = errors.New("environment storage backend not found")
	ErrVariableReadOnly    = errors.New("environment variable is read-only")
	ErrInvalidVariableName = errors.New("invalid environment variable name")
)

type Variable struct {
	ID           registry.ID       `json:"id"`
	Meta         registry.Metadata `json:"meta,omitempty"`
	Name         string            `json:"variable"`
	DefaultValue string            `json:"default,omitempty"`
	ReadOnly     bool              `json:"readonly,omitempty"`
	StorageID    registry.ID       `json:"storage"`
}

type Storage interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name, value string) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) (map[string]string, error)
}

type Registry interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name string, value string) error
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
