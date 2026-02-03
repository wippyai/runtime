// Package extension defines the runtime extension manifest.
package extension

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
)

const (
	// ABI is the supported extension ABI version.
	ABI = 1
	// Symbol is the exported symbol name extensions must provide.
	Symbol = "WippyExtension"
)

// Manifest describes a loadable extension.
// Extensions are expected to export a variable named Symbol with this type.
type Manifest struct {
	Init       func(context.Context) (context.Context, error)
	Name       string
	Version    string
	Components []boot.Component
	ABI        int
}
