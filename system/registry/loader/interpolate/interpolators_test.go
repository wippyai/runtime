package interpolate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockEnvRegistry implements envapi.Registry for testing
type MockEnvRegistry struct {
	variables map[string]string
}

func NewMockEnvRegistry(vars map[string]string) *MockEnvRegistry {
	return &MockEnvRegistry{
		variables: vars,
	}
}

func (m *MockEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if value, exists := m.variables[name]; exists {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) GetEventually(_ context.Context, name string) (string, error) {
	if value, exists := m.variables[name]; exists {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) Set(_ context.Context, name string, value string) error {
	m.variables[name] = value
	return nil
}

func (m *MockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	result := make(map[string]string)
	for k, v := range m.variables {
		result[k] = v
	}
	return result, nil
}

func TestLoadVars(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		ctx      EntryContext
		expected string
	}{
		{
			name:  "simple replacement",
			input: "Hello ${NAME}!",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"NAME": "World"})),
			},
			expected: "Hello World!",
		},
		{
			name:  "multiple replacements",
			input: "Port: ${PORT}, Env: ${ENV}",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"PORT": "8080", "ENV": "production"})),
			},
			expected: "Port: 8080, Env: production",
		},
		{
			name:  "no replacement",
			input: "No variables here.",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"PORT": "8080"})),
			},
			expected: "No variables here.",
		},
		{
			name:  "unknown variable",
			input: "value: ${UNKNOWN}",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"PORT": "8080"})),
			},
			expected: "value: ${UNKNOWN}", // Unresolved variable is left as is
		},
		{
			name:  "empty variables",
			input: "value: ${EMPTY}",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{})),
			},
			expected: "value: ${EMPTY}",
		},
		{
			name:  "variable with default value",
			input: "token: ${ENV_TOKEN_KEY:-secretkey123}",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{})),
			},
			expected: "token: secretkey123",
		},
		{
			name:  "variable with default value when variable exists",
			input: "token: ${ENV_TOKEN_KEY:-secretkey123}",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"ENV_TOKEN_KEY": "customkey456"})),
			},
			expected: "token: customkey456",
		},
		{
			name:  "variable with default value with spaces",
			input: "token: ${ENV_TOKEN_KEY:-default value with spaces}",
			ctx: EntryContext{
				Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{})),
			},
			expected: "token: default value with spaces",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := LoadVars(tc.input, tc.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("LoadVars(%q) = %q; want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "testloadfile-*")
	require.NoError(t, err)

	err = errors.Join(
		os.MkdirAll(filepath.Join(tempDir, "listener"), fs.ModePerm),
		os.MkdirAll(filepath.Join(tempDir, "template"), fs.ModePerm),
		os.WriteFile(filepath.Join(tempDir, "listener", "listener.yaml"), []byte("listener content"), 0600),
		os.WriteFile(filepath.Join(tempDir, "template", "template.html"), []byte("template content"), 0600),
		os.WriteFile(filepath.Join(tempDir, "main.yaml"), []byte("main content"), 0600),
	)
	require.NoError(t, err)

	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)

	configFile := filepath.Join("listener", "listener.yaml")
	mainFile := "main.yaml"

	tests := []struct {
		name        string
		input       string
		ctx         EntryContext
		expectedOut string
		expectErr   assert.ErrorAssertionFunc
	}{
		{
			name:  "valid relative path",
			input: "file://listener/listener.yaml",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       root.FS(),
			},
			expectedOut: "listener content",
			expectErr:   assert.NoError,
		},
		{
			name:  "valid relative path with directory",
			input: "file://../template/template.html",
			ctx: EntryContext{
				Filename: configFile,
				FS:       root.FS(),
			},
			expectErr:   assert.NoError,
			expectedOut: "template content",
		},
		{
			name:  "valid absolute path",
			input: "file:///listener/listener.yaml",
			ctx: EntryContext{
				Filename: configFile,
				FS:       root.FS(),
			},
			expectedOut: "listener content",
			expectErr:   assert.NoError,
		},
		{
			name:  "absolute path outside root",
			input: "file:///../outside.txt",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       root.FS(),
			},
			expectedOut: "file:///../outside.txt",
			expectErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorIs(t, err, fs.ErrNotExist, i...)
			},
		},
		{
			name:  "relative path outside root",
			input: "file://../outside.txt",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       root.FS(),
			},
			expectedOut: "file://../outside.txt",
			expectErr: assert.ErrorAssertionFunc(func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "path traversal detected", i...)
			}),
		},
		{
			name:  "file not found",
			input: "file://notfound.txt",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       root.FS(),
			},
			expectedOut: "file://notfound.txt",
			expectErr:   assert.Error, // Expect file-error
		},
		{
			name:  "no file protocol",
			input: "no_protocol",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       root.FS(),
			},
			expectedOut: "no_protocol",
			expectErr:   assert.NoError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := LoadFile(tt.input, tt.ctx)
			assert.Equal(t, tt.expectedOut, out)
			tt.expectErr(t, err)
		})
	}
}

func TestEnvFieldInterpolation(t *testing.T) {
	t.Run("_env field resolves to env variable value", func(t *testing.T) {
		h := NewEntryInterpolator(nil)
		ctx := EntryContext{
			Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"ACCESS_KEY_ID_CUSTOM": "my-access-key"})),
		}
		input := map[string]interface{}{
			"access_key_id_env": "ACCESS_KEY_ID_CUSTOM",
		}
		out, err := h.interpolateMap(input, ctx)
		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"access_key_id_env": "my-access-key"}, out)
	})

	t.Run("_env field with missing env variable and no default", func(t *testing.T) {
		h := NewEntryInterpolator(nil)
		ctx := EntryContext{
			Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{})),
		}
		input := map[string]interface{}{
			"access_key_id_env": "ACCESS_KEY_ID_CUSTOM",
		}
		out, err := h.interpolateMap(input, ctx)
		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"access_key_id_env": "ACCESS_KEY_ID_CUSTOM"}, out)
	})

	t.Run("_env field with default value", func(t *testing.T) {
		h := NewEntryInterpolator(nil)
		ctx := EntryContext{
			Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{})),
		}
		input := map[string]interface{}{
			"access_key_id_env": "ACCESS_KEY_ID_CUSTOM:-default-key",
		}
		out, err := h.interpolateMap(input, ctx)
		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"access_key_id_env": "default-key"}, out)
	})

	t.Run("_env field with env variable and default value", func(t *testing.T) {
		h := NewEntryInterpolator(nil)
		ctx := EntryContext{
			Context: envapi.WithRegistry(context.Background(), NewMockEnvRegistry(map[string]string{"ACCESS_KEY_ID_CUSTOM": "real-key"})),
		}
		input := map[string]interface{}{
			"access_key_id_env": "ACCESS_KEY_ID_CUSTOM:-default-key",
		}
		out, err := h.interpolateMap(input, ctx)
		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"access_key_id_env": "real-key"}, out)
	})
}
