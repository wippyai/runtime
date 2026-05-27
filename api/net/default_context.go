// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

// defaultNetworkKey carries the selected overlay network ID on the
// FrameContext. Inherit:true so forked child frames automatically pick up
// the parent's selection without explicit copying.
var defaultNetworkKey = &ctxapi.Key{Name: "net.default_network", Inherit: true}

// appDefaultNetworkKey carries the app-wide fallback overlay network ID on
// the AppContext. Set once at boot from .wippy.yaml (network_service.default_network)
// and read by runtime dispatchers when no per-entry or per-call override is
// present. Write-once, like every other AppContext value.
var appDefaultNetworkKey = &ctxapi.Key{Name: "net.app_default_network"}

// Default network selection is layered. Consumers of GetDefaultNetwork see
// the effective value after the following precedence has been applied:
//
//  1. per-call   – options.network merged at the spawn-site
//  2. per-entry  – meta.options.network on the process/function entry
//  3. app-level  – network_service.default_network in .wippy.yaml
//
// Tiers 1 and 2 share the same options bag and are already merged by the
// caller; the runtime dispatchers (process, lua, wasm managers) fall back to
// tier 3 via AppDefaultNetwork when the merged bag has no "network" key.
// The resulting ID is written onto the new FrameContext via DefaultNetworkPair
// and inherited through every fork.

// GetDefaultNetwork retrieves the effective overlay network ID from the
// FrameContext. Returns empty string when no overlay is selected; consumers
// treat that as "clearnet".
func GetDefaultNetwork(ctx context.Context) string {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ""
	}
	v, ok := fc.Get(defaultNetworkKey)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// DefaultNetworkPair returns a context.Pair for injecting the default
// network into a Task/Start Context slice. The Inherit flag on the
// underlying key guarantees propagation through subsequent frame forks.
func DefaultNetworkPair(networkID string) ctxapi.Pair {
	return ctxapi.Pair{Key: defaultNetworkKey, Value: networkID}
}

// WithAppDefaultNetwork attaches the app-wide fallback overlay network ID
// to the AppContext. Intended for boot-time wiring only. No-op when the
// AppContext is absent or the key is already set (AppContext is write-once).
func WithAppDefaultNetwork(ctx context.Context, networkID string) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(appDefaultNetworkKey) == nil {
		ac.With(appDefaultNetworkKey, networkID)
	}
	return ctx
}

// AppDefaultNetwork returns the app-wide fallback overlay network ID from
// the AppContext, or empty string when unset. Dispatchers call this when
// per-entry and per-call selectors both fail to produce an ID.
func AppDefaultNetwork(ctx context.Context) string {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ""
	}
	if v, ok := ac.Get(appDefaultNetworkKey).(string); ok {
		return v
	}
	return ""
}

// ResolveOverlayID picks the overlay network ID to apply to a newly-spawned
// task or process. Precedence: per-call/per-entry options bag ("network"
// key) first, then the app-wide boot default, then empty ("clearnet").
// Prefer ApplyOverlayPair for spawn sites — ResolveOverlayID is exported for
// callers that need the raw ID.
func ResolveOverlayID(ctx context.Context, options attrs.Attributes) string {
	if options != nil {
		if id := options.GetString(OptionKeyNetwork, ""); id != "" {
			return id
		}
	}
	return AppDefaultNetwork(ctx)
}

// ApplyOverlayPair is the single entry point spawn sites use to decorate a
// new task/process context with its overlay network selection. It resolves
// the ID (per-call options, then app default), verifies the network is
// registered, and appends DefaultNetworkPair to pairs. When no overlay is
// selected the input is returned unchanged. Returns ErrNetworkNotFound when
// a selector names an unregistered network.
func ApplyOverlayPair(ctx context.Context, options attrs.Attributes, pairs []ctxapi.Pair) ([]ctxapi.Pair, error) {
	networkID := ResolveOverlayID(ctx, options)
	if networkID == "" {
		return pairs, nil
	}
	reg := GetNetworkRegistry(ctx)
	if reg == nil || !reg.HasNetwork(registry.ParseID(networkID)) {
		return nil, ErrNetworkNotFound
	}
	return append(pairs, DefaultNetworkPair(networkID)), nil
}
