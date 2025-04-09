package interpolate

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

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
				Vars: Variables{"NAME": "World"},
			},
			expected: "Hello World!",
		},
		{
			name:  "multiple replacements",
			input: "Port: ${PORT}, Env: ${ENV}",
			ctx: EntryContext{
				Vars: Variables{"PORT": "8080", "ENV": "production"},
			},
			expected: "Port: 8080, Env: production",
		},
		{
			name:  "no replacement",
			input: "No variables here.",
			ctx: EntryContext{
				Vars: Variables{"PORT": "8080"},
			},
			expected: "No variables here.",
		},
		{
			name:  "unknown variable",
			input: "value: ${UNKNOWN}",
			ctx: EntryContext{
				Vars: Variables{"PORT": "8080"},
			},
			expected: "value: ${UNKNOWN}", // Unresolved variable is left as is
		},
		{
			name:  "empty variables",
			input: "value: ${EMPTY}",
			ctx: EntryContext{
				Vars: Variables{},
			},
			expected: "value: ${EMPTY}",
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
	mapFS := fstest.MapFS{
		"listener/listener.yaml": {Data: []byte("listener content")},
		"template/template.html": {Data: []byte("template content")},
		"main.yaml":              {Data: []byte("main content")},
	}

	configFile := filepath.Join("listener", "listener.yaml")
	mainFile := filepath.Join("main.yaml")

	tests := []struct {
		name        string
		input       string
		ctx         EntryContext
		expectedOut string
		expectErr   bool
	}{
		{
			name:  "valid relative path",
			input: "file://listener/listener.yaml",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       mapFS,
			},
			expectedOut: "listener content",
			expectErr:   false,
		},
		{
			name:  "valid relative path with directory",
			input: "file://../template/template.html",
			ctx: EntryContext{
				Filename: configFile,
				FS:       mapFS,
			},
			expectErr:   false,
			expectedOut: "template content",
		},
		{
			name:  "valid absolute path",
			input: "file:///listener/listener.yaml",
			ctx: EntryContext{
				Filename: configFile,
				FS:       mapFS,
			},
			expectedOut: "listener content",
			expectErr:   false,
		},
		{
			name:  "invalid absolute path outside root",
			input: fmt.Sprintf("file://%s", filepath.Join(".", "..", "outside.txt")),
			ctx: EntryContext{
				Filename: mainFile,
				FS:       mapFS,
			},
			expectedOut: fmt.Sprintf("file://%s [file-error: file path '%s' is outside of the root directory]", filepath.Join(".", "..", "outside.txt"), filepath.Join(".", "..", "outside.txt")),
			expectErr:   true, // Expect file-error
		},
		{
			name:  "relative path outside root",
			input: "file://../outside.txt",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       mapFS,
			},
			expectedOut: "file://../outside.txt [file-error: file path '../outside.txt' is outside of the root directory]",
			expectErr:   true, // Expect file-error
		},
		{
			name:  "file not found",
			input: "file://notfound.txt",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       mapFS,
			},
			expectedOut: "file://notfound.txt [file-error: failed to read file 'notfound.txt': ",
			expectErr:   true, // Expect file-error
		},
		{
			name:  "no file protocol",
			input: "no_protocol",
			ctx: EntryContext{
				Filename: mainFile,
				FS:       mapFS,
			},
			expectedOut: "no_protocol",
			expectErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := LoadFile(tt.input, tt.ctx)

			if tt.expectErr {
				assert.True(t, strings.Contains(out, "file-error"), "Expected 'file-error' in error message")
			} else {
				assert.Equal(t, tt.expectedOut, out, "Expected output does not match")
			}
		})
	}
}
