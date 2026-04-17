// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"path/filepath"

	envapi "github.com/wippyai/runtime/api/env"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Driver creates an overlay network Service from a registry entry. Each
// overlay kind (socks5, i2p, tailscale, ...) is implemented as its own
// package exposing a Driver; the Manager routes entries to the matching
// driver by registry.Kind.
type Driver interface {
	// Kind returns the registry kind this driver handles.
	Kind() registry.Kind
	// Create instantiates the Service described by entry. Drivers should
	// use deps to decode config and resolve external references
	// (env vars, state directories, etc.) rather than reaching out on
	// their own.
	Create(ctx context.Context, entry registry.Entry, deps Deps) (netapi.Service, error)
}

// Deps carries the shared environment every driver may need when building a
// Service: the payload transcoder for config decoding, an env registry for
// indirect credentials, a logger, and the base state directory under which
// drivers that persist local files place their subdirectories.
type Deps struct {
	Transcoder payload.Transcoder
	Env        envapi.Registry
	Logger     *zap.Logger
	StateDir   string
}

// DriverStateDir returns the canonical per-driver state subdirectory under
// Deps.StateDir, or empty when no base was configured so the driver's own
// default applies.
func (d Deps) DriverStateDir(driver, node string) string {
	if d.StateDir == "" {
		return ""
	}
	if node == "" {
		return filepath.Join(d.StateDir, driver)
	}
	return filepath.Join(d.StateDir, driver, node)
}
