package interpolate

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// FileProtocol is the prefix used to identify file-based configuration values.
// Values starting with this prefix will be interpreted as file paths and their
// contents will be loaded.
const FileProtocol = "file://"

// LoadVars replaces placeholders of the form "${variable}" in the input string with
// their corresponding values from the provided context. If no such placeholders are found,
// the string is returned unchanged.
func LoadVars(s string, ctx interface{}) (string, error) {
	rctx, ok := ctx.(EntryContext)
	if !ok {
		return s, nil // Invalid context, skip
	}

	if !strings.Contains(s, "${") {
		return s, nil // No variable placeholders, skip
	}

	result := s
	for k, v := range rctx.Vars {
		placeholder := "${" + k + "}"
		result = strings.ReplaceAll(result, placeholder, v)
	}

	return result, nil
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

	if !strings.HasPrefix(s, FileProtocol) {
		return s, nil // Not a file path, skip it
	}

	filePath := strings.TrimPrefix(s, FileProtocol)

	// Convert to system-specific path format for processing
	systemPath := filepath.FromSlash(filePath)
	var fullPath string

	fmt.Println(systemPath)

	if filepath.IsAbs(systemPath) {
		// Handle absolute paths
		rel, err := filepath.Rel("/", filepath.Clean(filePath))
		if err != nil {
			return "", fmt.Errorf("resolve relative path: %w", err)
		}
		fullPath = rel
	} else {
		// Relative path, make it relative to the context directory
		var fileDir string
		if rCtx.Filename != "" {
			fileDir = filepath.Dir(rCtx.Filename)
		}
		fullPath = filepath.Join(fileDir, systemPath)
	}

	// Clean the path to resolve any ".." segments
	fullPath = filepath.Clean(fullPath)

	// Convert back to forward slashes for io/fs package
	// fs.ReadFile always expects forward slashes, even on Windows
	fsPath := filepath.ToSlash(fullPath)

	data, err := fs.ReadFile(rCtx.FS, fsPath)
	if err != nil {
		return s + fmt.Sprintf(" [file-error: failed to read file '%s': %v]", filePath, err), err
	}

	return string(data), nil
}
