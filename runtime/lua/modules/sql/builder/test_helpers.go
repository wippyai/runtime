package builder

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}
