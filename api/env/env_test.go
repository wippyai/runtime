// Package env provides environment variable access and management.
package env

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	contextapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
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
		{"accepted", "", EnvAccept, "accept"},
		{"rejected", "", EnvReject, "reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, tt.system)
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, tt.kind)
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

func TestError_Interface(t *testing.T) {
	t.Run("sentinel errors implement Error interface", func(t *testing.T) {
		errVar := ErrVariableNotFound
		assert.Equal(t, "environment variable not found", errVar.Error())
		assert.Equal(t, "NotFound", errVar.Kind().String())
		assert.False(t, errVar.Retryable().Bool())
		assert.Nil(t, errVar.Details())
		assert.Nil(t, errors.Unwrap(errVar))
	})

	t.Run("additional sentinel errors", func(t *testing.T) {
		assert.Equal(t, "invalid storage ID format, must have both namespace and name", ErrInvalidStorageID.Error())
		assert.Equal(t, "router storage must have at least one storage", ErrEmptyStorageList.Error())
		assert.Equal(t, "storage is read-only", ErrStorageReadOnly.Error())
		assert.Equal(t, "at least one storage must be provided", ErrNoStorages.Error())
		assert.Equal(t, "file path must not be empty", ErrEmptyFilePath.Error())
	})

	t.Run("Validate invalid variable name includes details", func(t *testing.T) {
		v := Variable{Name: "bad-name"}
		err := v.Validate()
		require.Error(t, err)

		var apiErr apierror.Error
		ok := errors.As(err, &apiErr)
		require.True(t, ok)
		assert.Contains(t, apiErr.Error(), "invalid environment variable name")
		assert.Equal(t, apierror.Invalid, apiErr.Kind())

		details := apiErr.Details()
		require.NotNil(t, details)
		varName, _ := details.Get("variable")
		assert.Equal(t, "bad-name", varName)
		reason, _ := details.Get("reason")
		assert.Equal(t, "must only contain alphanumeric characters (a-z, A-Z, 0-9) and underscores", reason)
	})
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
				ID:           registry.NewID("env", "var1"),
				Meta:         attrs.Bag{"source": "config"},
				Name:         "DATABASE_URL",
				DefaultValue: "localhost:5432",
				ReadOnly:     true,
				StorageID:    registry.NewID("storage", "file"),
			},
			wantErr: false,
		},
		{
			name: "minimal variable",
			v: Variable{
				ID:        registry.NewID("", ""),
				Name:      "API_KEY",
				StorageID: registry.NewID("storage", "env"),
			},
			wantErr: false,
		},
		{
			name: "with default value only",
			v: Variable{
				ID:           registry.NewID("", ""),
				Name:         "LOG_LEVEL",
				DefaultValue: "info",
				StorageID:    registry.NewID("storage", "mem"),
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
		errMsg  string
		v       Variable
		wantErr bool
	}{
		{
			name: "valid variable",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.NewID("storage", "file"),
			},
			wantErr: false,
		},
		{
			name: "valid with numbers",
			v: Variable{
				Name:      "VAR_123",
				StorageID: registry.NewID("storage", "file"),
			},
			wantErr: false,
		},
		{
			name: "valid with underscores",
			v: Variable{
				Name:      "MY_VAR_NAME",
				StorageID: registry.NewID("storage", "file"),
			},
			wantErr: false,
		},
		{
			name: "invalid character dash",
			v: Variable{
				Name:      "INVALID-NAME",
				StorageID: registry.NewID("storage", "file"),
			},
			wantErr: true,
			errMsg:  "must only contain alphanumeric characters",
		},
		{
			name: "invalid character dot",
			v: Variable{
				Name:      "INVALID.NAME",
				StorageID: registry.NewID("storage", "file"),
			},
			wantErr: true,
			errMsg:  "must only contain alphanumeric characters",
		},
		{
			name: "invalid character space",
			v: Variable{
				Name:      "INVALID NAME",
				StorageID: registry.NewID("storage", "file"),
			},
			wantErr: true,
			errMsg:  "must only contain alphanumeric characters",
		},
		{
			name: "missing storage namespace",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.NewID("", "file"),
			},
			wantErr: true,
			errMsg:  "must have both namespace and name",
		},
		{
			name: "missing storage name",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.NewID("storage", ""),
			},
			wantErr: true,
			errMsg:  "must have both namespace and name",
		},
		{
			name: "empty storage ID",
			v: Variable{
				Name:      "VALID_NAME",
				StorageID: registry.NewID("", ""),
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
