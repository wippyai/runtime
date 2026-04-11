// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/scheduler"
)

func PIDGen() boot.Component {
	return boot.New(boot.P{
		Name: PIDGenName,
		Load: func(ctx context.Context) (context.Context, error) {
			uniqGen := uniqid.NewGenerator()

			// Get node ID from the relay node (set during bootstrap).
			// For clustered nodes this is the configured node name;
			// for standalone it defaults to "local".
			var nodeID pid.NodeID
			if node := relayapi.GetNode(ctx); node != nil {
				nodeID = node.ID()
				// "local" is the default for non-clustered nodes;
				// omit it from PIDs to keep them short.
				if nodeID == "local" {
					nodeID = ""
				}
			}

			gen := uniqid.NewPIDGenerator(uniqGen, nodeID)
			return process.WithPIDGenerator(ctx, gen), nil
		},
	})
}

func Dispatcher() boot.Component {
	return boot.New(boot.P{
		Name: DispatcherName,
		Load: func(ctx context.Context) (context.Context, error) {
			// Create dispatcher registry for this application instance
			reg := scheduler.NewRegistry()
			if err := dispatcherapi.WithRegistry(ctx, reg); err != nil {
				return ctx, err
			}
			return ctx, nil
		},
	})
}
