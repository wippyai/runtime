// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
)

// A reused PID string does not make a replacement process the issuer of a
// request that was still being admitted when the previous process completed.
func TestRemoteMonitorCannotAdoptReplacementCaller(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			f := newMonitorSenderFixture(t)
			f.afterAdmission = func(*relay.Package) error {
				// Completion also sends an exact release. Avoid recursively completing
				// the caller while processing that cleanup control.
				f.afterAdmission = nil
				f.local.Complete(f.caller, &runtime.Result{})
				if replace {
					require.NoError(t, f.local.Register(f.caller))
				}
				return nil
			}
			require.ErrorIs(t, f.local.Monitor(f.caller, f.target), topapi.ErrPIDNotRegistered)
			sh := f.local.getShard(f.caller.String())
			sh.mu.RLock()
			defer sh.mu.RUnlock()
			state, exists := sh.processes[f.caller.String()]
			require.Equal(t, replace, exists)
			if exists {
				require.Empty(t, state.watching)
				require.Empty(t, state.remoteWatching)
			}
			require.Len(t, f.controls, 2)
			require.Equal(t, topapi.MonitorRelease, f.controls[1].kind)
			require.Equal(t, f.controls[0].reference, f.controls[1].reference)
		})
	}
}
