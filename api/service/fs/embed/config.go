package embed

import (
	"github.com/wippyai/runtime/api/registry"
)

const Kind registry.Kind = "fs.embed"

// Config represents configuration for an embedded filesystem from a pack.
// The filesystem is loaded from pack resources using the entry ID.
type Config struct {
	// No configuration needed - the entry ID is used to locate the resource
}
