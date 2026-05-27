// SPDX-License-Identifier: MPL-2.0

// Package directory provides directory service configuration.
package directory

import (
	"fmt"
	"io/fs"

	"github.com/wippyai/runtime/api/registry"
)

// Kind identifies directory filesystem entries in the registry.
const Kind registry.Kind = "fs.directory"

// Config represents configuration for a filesystem directory
type Config struct {
	Directory  string `json:"directory"`
	Mode       string `json:"mode"`
	Type       string `json:"type"`
	Base       string `json:"base"`
	parsedMode fs.FileMode
	AutoInit   bool `json:"auto_init"`
}

const (
	// BaseProject resolves relative paths against the process working directory.
	BaseProject = "project"
	// BaseModule resolves relative paths against the owning module load root.
	BaseModule = "module"
)

func (c *Config) GetMode() fs.FileMode {
	if c.parsedMode == 0 {
		return 0755 // Default mode if not set
	}

	return c.parsedMode
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Directory == "" {
		return ErrEmptyDirectoryPath
	}

	if c.Base != "" && c.Base != BaseProject && c.Base != BaseModule {
		return NewInvalidBaseError(c.Base)
	}

	if c.Mode != "" {
		// Convert mode string to fs.FileMode and validate
		mode, err := parseFileMode(c.Mode)
		if err != nil {
			return err
		}
		// Store parsed mode for future use
		c.parsedMode = mode
	} else {
		// Default to 0755 if not specified
		c.parsedMode = 0755
	}

	return nil
}

// parseFileMode converts a string like "0755" to fs.FileMode
func parseFileMode(s string) (fs.FileMode, error) {
	var mode uint32
	if _, err := fmt.Sscanf(s, "%o", &mode); err != nil {
		return 0, NewInvalidModeFormatError(err)
	}
	return fs.FileMode(mode), nil
}
