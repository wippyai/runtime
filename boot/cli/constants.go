package cli

import "github.com/wippyai/runtime/api/boot"

const (
	// Component name (also used as config section name)
	RegistryClientName = "cli.registry"

	// Config keys for registry section
	RegistryURL boot.ConfigKey = "url"
)

const (
	// Default values
	DefaultRegistryURL = "https://modules.wippy.ai"
)
