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

// WithSecurityConfigE configures the security context and reports resolution
// or frame installation failures. Callers at a trust or startup boundary
// should use this form so a missing policy cannot silently fall back to the
// inherited context.
func WithSecurityConfigE(ctx context.Context, config *security.Config) (context.Context, error) {
	pairs, err := ResolveConfigPairs(ctx, config)
	if err != nil {
		return ctx, err
	}
	if len(pairs) == 0 {
		return ctx, nil
	}

	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctx, ctxapi.ErrNoFrameContext
	}
	if err := fc.SetMultiple(pairs...); err != nil {
		return ctx, err
	}

	return ctx, nil
}

// WithSecurityConfig configures the security context based on the provided
// configuration. It is retained for compatibility with callers that cannot
// return an error; startup and execution boundaries should prefer
// WithSecurityConfigE.
func WithSecurityConfig(ctx context.Context, config *security.Config) context.Context {
	ctx, _ = WithSecurityConfigE(ctx, config)

	return ctx
}
