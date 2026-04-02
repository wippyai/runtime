// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var defaultNetworkKey = &ctxapi.Key{Name: "net.default_network", Inherit: true}

// SetDefaultNetwork sets the default overlay network ID on the FrameContext.
// This value is inherited by child function executions.
func SetDefaultNetwork(ctx context.Context, networkID string) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(defaultNetworkKey, networkID)
}

// GetDefaultNetwork retrieves the default overlay network ID from the FrameContext.
// Returns empty string if no default is set.
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

// DefaultNetworkPair returns a context.Pair for injecting the default network
// into a Task.Context slice.
func DefaultNetworkPair(networkID string) ctxapi.Pair {
	return ctxapi.Pair{Key: defaultNetworkKey, Value: networkID}
}
