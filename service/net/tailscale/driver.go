// SPDX-License-Identifier: MPL-2.0

package tailscale

import (
	"context"

	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	entryutil "github.com/wippyai/runtime/internal/entry"
	netservice "github.com/wippyai/runtime/service/net"
)

// Driver implements netservice.Driver for the Tailscale overlay kind. It
// resolves env-var auth keys through deps.Env and applies the canonical
// per-node state directory under deps.StateDir before starting tsnet.
type Driver struct{}

// NewDriver returns a ready-to-register Tailscale Driver.
func NewDriver() Driver { return Driver{} }

// Kind returns netapi.KindTailscale.
func (Driver) Kind() registry.Kind { return netapi.KindTailscale }

// Create decodes the entry into a TailscaleConfig, resolves its auth key
// (from AuthKeyEnv when AuthKey is empty), fills in the state directory
// default, and starts the tsnet node.
func (d Driver) Create(ctx context.Context, entry registry.Entry, deps netservice.Deps) (netapi.Service, error) {
	cfg, err := entryutil.DecodeEntryConfig[netapi.TailscaleConfig](ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, netservice.NewDecodeConfigError("tailscale", err)
	}
	if err := resolveAuthKey(ctx, cfg, deps); err != nil {
		return nil, err
	}
	resolveStateDir(cfg, entry.ID, deps)
	return NewService(cfg)
}

// resolveAuthKey fills cfg.AuthKey from the env registry when only
// AuthKeyEnv is set. Keeps the driver agnostic of where secrets live.
func resolveAuthKey(ctx context.Context, cfg *netapi.TailscaleConfig, deps netservice.Deps) error {
	if cfg.AuthKey != "" || cfg.AuthKeyEnv == "" {
		return nil
	}
	if deps.Env == nil {
		return netservice.NewEnvRegistryUnavailableError(cfg.AuthKeyEnv)
	}
	value, err := deps.Env.Get(ctx, cfg.AuthKeyEnv)
	if err != nil {
		return netservice.NewAuthKeyLookupError(cfg.AuthKeyEnv, err)
	}
	cfg.AuthKey = value
	return nil
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
