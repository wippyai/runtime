// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"github.com/wippyai/runtime/api/registry"
)

// Kind identifies embedded filesystem entries in the registry.
const Kind registry.Kind = "fs.embed"

// Config represents configuration for an embedded filesystem from a pack.
// The filesystem is loaded from pack resources using the entry ID.
type Config struct {
	// Digest is the aggregate content hash of the resource's files, stamped
	// at pack time. It carries no locator information; it exists so a
	// registry-entry diff sees a content change even when nothing else about
	// this entry's declaration changed.
	Digest string `json:"digest,omitempty"`
}
