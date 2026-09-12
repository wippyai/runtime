// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/topology"
)

// lockExclusionContext preserves cross-scope serialization during normal
// operation. During explicit retirement, a sealed AND drained guard is stronger
// evidence: no weaker registration can race the conflict observation. Protocol
// exclusion/vote work may continue; this never admits application mutations.
func (st *strongState) lockExclusionContext(ctx context.Context, name string) (func(), error) {
	release, err := st.nameGuard.LockContext(ctx, name)
	if !errors.Is(err, topology.ErrNameAdmissionClosed) {
		return release, err
	}
	run := st.svc.reconciler.Load()
	if run == nil || !run.mutationsSealed.Load() {
		return nil, err
	}
	if err := st.nameGuard.Close(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return func() {}, nil
}
