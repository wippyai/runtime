// SPDX-License-Identifier: MPL-2.0

package tailscale

import (
	"context"

	env "github.com/wippyai/runtime/api/env"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	netservice "github.com/wippyai/runtime/service/net"
	entryutil "github.com/wippyai/runtime/system/entry"
)

// Driver implements netservice.Driver for the Tailscale overlay kind. It
// resolves env-var auth keys through deps.Env and applies the canonical
// per-node state directory under deps.StateDir before starting tsnet.
type Driver struct{}

// NewDriver returns a ready-to-register Tailscale Driver.
func NewDriver() Driver { return Driver{} }

// Kind returns netapi.KindTailscale.
func (Driver) Kind() registry.Kind { return netapi.KindTailscale }

// Create decodes the entry into a TailscaleConfig, fills in the state
// directory default, and starts the tsnet node. When deps.Env is present it is
// attached to the context so the central decode pass resolves an auth_key_env
// directive into cfg.AuthKey.
func (d Driver) Create(ctx context.Context, entry registry.Entry, deps netservice.Deps) (netapi.Service, error) {
	if deps.Env != nil {
		ctx = env.WithRegistry(ctx, deps.Env)
	}
	cfg, err := entryutil.DecodeEntryConfig[netapi.TailscaleConfig](ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, netservice.NewDecodeConfigError("tailscale", err)
	}
	resolveStateDir(cfg, entry.ID, deps)
	return NewService(cfg)
}

// resolveStateDir defaults an unset cfg.StateDir to the per-node
// subdirectory of deps.StateDir. The per-node segment is Hostname when
// provided (the identity tsnet registers under) and falls back to the
// registry entry name, which is always unique within a namespace.
func resolveStateDir(cfg *netapi.TailscaleConfig, id registry.ID, deps netservice.Deps) {
	if cfg.StateDir != "" {
		return
	}
	node := cfg.Hostname
	if node == "" {
		node = id.Name
	}
	cfg.StateDir = deps.DriverStateDir("tailscale", node)
}
