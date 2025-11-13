// Package directory provides directory service configuration.
package directory

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/ponyruntime/pony/api/registry"
)

const Kind registry.Kind = "fs.directory"

// TypeNameEmbed specifies special type of the directory fs - embed.
const TypeNameEmbed = "embed"

// Config represents configuration for a filesystem directory
type Config struct {
	// Directory is the root path for this filesystem
	Directory string `json:"directory"`
	// AutoInit creates directory path if it does not exist.
	AutoInit bool `json:"auto_init"`

	// Mode specifies the filesystem permissions
	// Examples:
	// - "0444" (r--r--r--) Read-only for everyone
	// - "0700" (rwx------) Full access for owner only
	// - "0755" (rwxr-xr-x) Read/execute for group/others, full access for owner
	Mode string `json:"mode"`

	// Type specifies special type of the systems (for now only 'embed').
	Type string `json:"type"`

	// Parsed mode value
	parsedMode fs.FileMode
}

func (c *Config) FileMode() fs.FileMode {
	if c.parsedMode == 0 {
		return 0755 // Default mode if not set
	}

	return c.parsedMode
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Directory == "" {
		return errors.New("directory path cannot be empty")
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
		return 0, fmt.Errorf("invalid mode format: %w", err)
	}
	return fs.FileMode(mode), nil
}
