// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// FrameResolverOrderNetwork is the apply order of the network overlay resolver.
// Lower orders run first; the resulting pairs are applied to the frame in that
// order, so a later resolver's key wins a collision.
const FrameResolverOrderNetwork = 200

// FrameResolvers creates the frame-context resolver registry. Frame-decorating
// options register a resolver here at boot; the function and process
// dispatchers apply the whole set generically, so no dispatcher depends on a
// specific subsystem.
func FrameResolvers() boot.Component {
	return boot.New(boot.P{
		Name:      FrameResolversName,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			resolvers := ctxapi.NewFrameResolvers()
			if err := resolvers.RegisterClaim(ttyapi.FrameResolverClaimTTY, ttyapi.TerminalOptionSelected); err != nil {
				return nil, fmt.Errorf("register terminal frame claim: %w", err)
			}
			return ctxapi.WithFrameResolvers(ctx, resolvers), nil
		},
	})
}
