package interpolate

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	envapi "github.com/ponyruntime/pony/api/env"
)

// LoadVars replaces placeholders of the form "${variable}" in the input string with
// their corresponding values from the environment registry. If no such placeholders are found,
// the string is returned unchanged.
func LoadVars(s string, ctx interface{}) (string, error) {
	rctx, ok := ctx.(EntryContext)
	if !ok {
		return s, nil // Invalid context, skip
	}

	if !strings.Contains(s, "${") {
		return s, nil // No variable placeholders, skip
	}

	// Get environment registry from context
	if rctx.Context == nil {
		return s, nil // No context available, skip
	}

	envRegistry := envapi.GetRegistry(rctx.Context)
	if envRegistry == nil {
		return s, nil // No environment registry available, skip
	}

	var result strings.Builder
	remaining := s

	for {
		start := strings.Index(remaining, "${")
		if start == -1 {
			// No more placeholders, append remaining text and break
			result.WriteString(remaining)
			break
		}

		// Append text before the placeholder
		result.WriteString(remaining[:start])

		// Find the end of the placeholder
		end := strings.Index(remaining[start:], "}")
		if end == -1 {
			// Unclosed placeholder, append the rest and break
			result.WriteString(remaining[start:])
			break
		}

		end += start
		placeholder := remaining[start : end+1]
		varContent := remaining[start+2 : end]

		// Parse variable name and default value (support shell-style ${VAR:-default} syntax)
		varName, defaultValue := parseVariableWithDefault(varContent)

		// Get variable value from environment registry

		value, err := envRegistry.GetEventually(context.Background(), varName)
		log.Println("variable >>> ", varName, " value:", value, " err:", err)
		if err != nil {
			// Variable not found, use default value if provided, otherwise leave placeholder as is
			if defaultValue != "" {
				result.WriteString(defaultValue)
			} else {
				result.WriteString(placeholder)
			}
		} else {
			// Replace placeholder with actual value
			result.WriteString(value)
		}

		// Continue with the remaining text after this placeholder
		remaining = remaining[end+1:]
	}

	return result.String(), nil
}

// parseVariableWithDefault parses a variable string that may contain a default value
// in the format "VAR_NAME" or "VAR_NAME:-default_value"
func parseVariableWithDefault(varContent string) (varName, defaultValue string) {
	// Look for the ":-" separator that indicates a default value
	if idx := strings.Index(varContent, ":-"); idx != -1 {
		varName = strings.TrimSpace(varContent[:idx])
		defaultValue = strings.TrimSpace(varContent[idx+2:])
		return varName, defaultValue
	}

	// No default value, return the entire content as the variable name
	return strings.TrimSpace(varContent), ""
}

// LoadFile attempts to load the content of a file specified by a "file://" protocol.
// If the input string does not start with "file://", it returns the string unchanged.
// It resolves file paths relative to the provided context, ensuring the resolved path
// remains within the root directory to prevent path traversal vulnerabilities.
// If the file is successfully read, it returns the file's content; otherwise,
// it returns the original string appended with an error message.
func LoadFile(s string, ctx interface{}) (string, error) {
	rCtx, ok := ctx.(EntryContext)
	if !ok {
		return s, nil // Invalid context, skip
	}

	// Check if the string starts with file protocol
	var filePath string
	var isRelative bool

	// fileProtocol is the prefix used to identify file-based configuration values.
	// Values starting with this prefix will be interpreted as file paths and their
	// contents will be loaded.
	const fileProtocol = "file://"

	switch {
	case strings.HasPrefix(s, fileProtocol+"/"):
		// Absolute path: file:///path/to/file
		filePath = strings.TrimPrefix(s, fileProtocol)
	case strings.HasPrefix(s, fileProtocol):
		// Relative path: file://path/to/file
		filePath = strings.TrimPrefix(s, fileProtocol)
		isRelative = true
	default:
		// Not a file URL, return unchanged
		return s, nil
	}

	if filePath == "" {
		return s, fmt.Errorf("empty file path in file:// URL")
	}

	var cleanPath string

	if isRelative {
		// For relative paths, resolve relative to the current config file's directory
		if rCtx.Filename == "" {
			return s, fmt.Errorf("cannot resolve relative file path without context filename")
		}

		// Get directory of current config file
		configDir := filepath.Dir(rCtx.Filename)

		// Join with the relative file path
		fullPath := filepath.Join(configDir, filePath)

		// Clean the path to resolve any .. or . components
		cleanPath = filepath.Clean(fullPath)
	} else {
		// For absolute paths, clean the path directly
		cleanPath = filepath.Clean(filePath)
	}

	// Convert to forward slashes for fs.FS compatibility (works on both Windows and Linux)
	cleanPath = filepath.ToSlash(cleanPath)

	// For fs.FS, paths must be relative (no leading slash)
	cleanPath = strings.TrimPrefix(cleanPath, "/")

	// Security check: ensure the cleaned path doesn't try to escape the FS root
	// This is particularly important for relative paths
	if strings.HasPrefix(cleanPath, "../") || cleanPath == ".." || strings.Contains(cleanPath, "/../") {
		return s, fmt.Errorf("path traversal detected in file path: %s", filePath)
	}

	// Validate that the path is not empty after cleaning
	if cleanPath == "" || cleanPath == "." {
		return s, fmt.Errorf("invalid file path: %s", filePath)
	}

	// Read the file using the provided filesystem
	content, err := fs.ReadFile(rCtx.FS, cleanPath)
	if err != nil {
		return s, fmt.Errorf("failed to read file %s: %w", cleanPath, err)
	}

	return string(content), nil
}
