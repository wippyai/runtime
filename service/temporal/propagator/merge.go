package propagator

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// MergeActivityContext applies Temporal activity context values onto the app context.
// It copies propagated values and security payloads into a new FrameContext on appCtx.
// Returns the merged context and a release function (no-op when nothing was applied).
func MergeActivityContext(appCtx context.Context, activityCtx context.Context) (context.Context, func(), error) {
	ctxValues := GetContextValues(activityCtx)
	secPayload := GetSecurityFromCtx(activityCtx)
	if len(ctxValues) == 0 && secPayload == nil {
		return appCtx, func() {}, nil
	}

	execCtx, fc := ctxapi.OpenFrameContextOn(appCtx, appCtx)
	release := func() { ctxapi.ReleaseFrameContext(fc) }

	if len(ctxValues) > 0 {
		values, err := ctxapi.GetOrCreateValues(execCtx)
		if err != nil {
			release()
			return appCtx, func() {}, err
		}
		for k, v := range ctxValues {
			values.Set(k, v)
		}
	}

	if secPayload != nil {
		if err := ApplySecurityPayload(execCtx, secPayload); err != nil {
			release()
			return appCtx, func() {}, err
		}
	}

	return execCtx, release, nil
}
