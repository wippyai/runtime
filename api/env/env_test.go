// Package env provides environment variable access and management.
package env

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "env"},
		{"storage register", "", StorageRegister, "storage.register"},
		{"storage delete", "", StorageDelete, "storage.delete"},
		{"storage update", "", StorageUpdate, "storage.update"},
		{"variable register", "", VariableRegister, "variable.register"},
		{"variable delete", "", VariableDelete, "variable.delete"},
		{"variable update", "", VariableUpdate, "variable.update"},
		{"accepted", "", Accepted, "accept"},
		{"rejected", "", Rejected, "reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, string(tt.system))
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, string(tt.kind))
			}
		})
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"variable not found", ErrVariableNotFound, "environment variable not found"},
		{"storage not found", ErrStorageNotFound, "environment storage backend not found"},
		{"variable read only", ErrVariableReadOnly, "environment variable is read-only"},
		{"invalid variable name", ErrInvalidVariableName, "invalid environment variable name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestVariable_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		v       Variable
		wantErr bool
	}{
		{
			name: "complete variable",
			v: Variable{
				ID:           registry.ID{NS: "env", Name: "var1"},
				Meta:         registry.Metadata{"source": "config"},
				Name:         "DATABASE_URL",
				DefaultValue: "localhost:5432",
				ReadOnly:     true,
				StorageID:    registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: false,
		},
		{
			name: "minimal variable",
			v: Variable{
				Name:      "API_KEY",
				StorageID: registry.ID{NS: "storage", Name: "env"},
			},
			wantErr: false,
		},
		{
			name: "with default value only",
			v: Variable{
				Name:         "LOG_LEVEL",
				DefaultValue: "info",
				StorageID:    registry.ID{NS: "storage", Name: "mem"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.v)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Variable
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.v, decoded)
		})
	}
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := contextapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetRegistry(ctx)
		assert.Nil(t, reg)
	})
}

func TestVariable_Validate(t *testing.T) {
	tests := []struct {
		name    string
		v       Variable
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid variable",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: false,
		},
		{
			name: "valid with numbers",
			v: Variable{
				Name:      "VAR_123",
				StorageID: registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: false,
		},
		{
			name: "valid with underscores",
			v: Variable{
				Name:      "MY_VAR_NAME",
				StorageID: registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: false,
		},
		{
			name: "invalid character dash",
			v: Variable{
				Name:      "INVALID-NAME",
				StorageID: registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: true,
			errMsg:  "must only contain alphanumeric characters",
		},
		{
			name: "invalid character dot",
			v: Variable{
				Name:      "INVALID.NAME",
				StorageID: registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: true,
			errMsg:  "must only contain alphanumeric characters",
		},
		{
			name: "invalid character space",
			v: Variable{
				Name:      "INVALID NAME",
				StorageID: registry.ID{NS: "storage", Name: "file"},
			},
			wantErr: true,
			errMsg:  "must only contain alphanumeric characters",
		},
		{
			name: "missing storage namespace",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.ID{NS: "", Name: "file"},
			},
			wantErr: true,
			errMsg:  "must have both namespace and name",
		},
		{
			name: "missing storage name",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.ID{NS: "storage", Name: ""},
			},
			wantErr: true,
			errMsg:  "must have both namespace and name",
		},
		{
			name: "empty storage ID",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.ID{},
			},
			wantErr: true,
			errMsg:  "must have both namespace and name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.v.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
