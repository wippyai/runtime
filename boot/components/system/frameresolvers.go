// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
)

// Frame-resolver apply order. Lower runs first; the resulting pairs are applied
// to the frame in this order, so a later resolver's key wins a collision.
const (
	FrameResolverOrderNetwork = 200
	FrameResolverOrderFSRoot  = 300
)

// FrameResolvers creates the frame-context resolver registry. Frame-decorating
// options (network overlay, filesystem root, ...) register a resolver here at
// boot; the function and process dispatchers apply the whole set generically,
// so no dispatcher depends on a specific subsystem.
func FrameResolvers() boot.Component {
	return boot.New(boot.P{
		Name:      FrameResolversName,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			return ctxapi.WithFrameResolvers(ctx, ctxapi.NewFrameResolvers()), nil
		},
	})
}
