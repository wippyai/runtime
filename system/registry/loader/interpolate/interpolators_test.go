package interpolate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
