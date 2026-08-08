// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
)

func actorConfigured(actor security.Actor) bool {
	return actor.ID != "" || len(actor.Meta) > 0
}

// WithSecurityConfig configures the security context based on the provided configuration.
func WithSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	pairs, _ := ResolveConfigPairs(ctx, config)
	if len(pairs) == 0 {
		return ctx
	}

	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctx
	}
	_ = fc.SetMultiple(pairs...)

	return ctx
}
