package loader

import (
	"fmt"
	"github.com/ponyruntime/pony/internal/utils"
	"github.com/stretchr/testify/assert"
	"path/filepath"
	"strings"
	"testing"
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
			input: "Value: ${UNKNOWN}",
			ctx: EntryContext{
				Vars: Variables{"PORT": "8080"},
			},
			expected: "Value: ${UNKNOWN}", // Unresolved variable is left as is
		},
		{
			name:  "empty variables",
			input: "Value: ${EMPTY}",
			ctx: EntryContext{
				Vars: Variables{},
			},
			expected: "Value: ${EMPTY}",
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

	files := map[string]string{
		"config/config.yaml":     "config content",
		"template/template.html": "template content",
		"main.yaml":              "main content",
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "test-load-file", files)
	defer cleanup()

	configFile := filepath.Join(rootDir, "config", "config.yaml")
	mainFile := filepath.Join(rootDir, "main.yaml")

	tests := []struct {
		name        string
		input       string
		ctx         EntryContext
		expectedOut string
		expectErr   bool
	}{
		{
			name:  "valid relative path",
			input: "file://config/config.yaml",
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: mainFile,
			},
			expectedOut: "config content",
			expectErr:   false,
		},
		{
			name:  "valid relative path with directory",
			input: "file://../template/template.html",
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: configFile,
			},
			expectErr:   false,
			expectedOut: "template content",
		},
		{
			name:  "valid absolute path",
			input: "file:///config/config.yaml",
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: configFile,
			},
			expectedOut: "config content",
			expectErr:   false,
		},
		{
			name:  "invalid absolute path outside root",
			input: fmt.Sprintf("file://%s", filepath.Join(rootDir, "..", "outside.txt")),
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: mainFile,
			},
			expectedOut: fmt.Sprintf("file://%s [file-error: file path '%s' is outside of the root directory]", filepath.Join(rootDir, "..", "outside.txt"), filepath.Join(rootDir, "..", "outside.txt")),
			expectErr:   true, // Expect file-error
		},
		{
			name:  "relative path outside root",
			input: "file://../outside.txt",
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: mainFile,
			},
			expectedOut: "file://../outside.txt [file-error: file path '../outside.txt' is outside of the root directory]",
			expectErr:   true, // Expect file-error
		},
		{
			name:  "file not found",
			input: "file://notfound.txt",
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: mainFile,
			},
			expectedOut: "file://notfound.txt [file-error: failed to read file 'notfound.txt': ",
			expectErr:   true, // Expect file-error
		},
		{
			name:  "no file protocol",
			input: "no_protocol",
			ctx: EntryContext{
				RootDir:  rootDir,
				Filename: mainFile,
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
