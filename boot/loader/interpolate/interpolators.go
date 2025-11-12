package interpolate

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

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
