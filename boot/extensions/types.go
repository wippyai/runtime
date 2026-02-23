// SPDX-License-Identifier: MPL-2.0

package extensions

import "github.com/wippyai/runtime/api/boot"

// Info summarizes a loaded extension.
type Info struct {
	Name    string
	Version string
	Path    string
}

// Result contains loaded extensions and their components.
type Result struct {
	Extensions []Info
	Components []boot.Component
}
