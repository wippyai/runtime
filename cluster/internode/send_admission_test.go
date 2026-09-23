// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestSendReportsAdmissionAcrossPeerLifecycle(t *testing.T) {
	for class := Class(0); class < numClasses; class++ {
		t.Run(class.String(), func(t *testing.T) {
			states := setupStateManager()
			m := &manager{nodeStates: states}
			checkRejected := func() {
				t.Helper()
				err := m.SendToNode("peer", []byte("request"), class)
				require.ErrorIs(t, err, ErrNodeNotManaged)
				var typed apierror.Error
				require.ErrorAs(t, err, &typed)
				require.Equal(t, apierror.Unavailable, typed.Kind())
				require.ErrorContains(t, err, "peer")
				require.Nil(t, states.GetNodeState("peer"))
			}
			checkRejected()
			for range 3 {
				states.CreateNodeState("peer")
				require.NoError(t, m.SendToNode("peer", []byte("accepted"), class))
				require.Equal(t, [][]byte{[]byte("accepted")}, drainAllData(states, "peer"))
				states.detachNodeState("peer")
				checkRejected()
			}
		})
	}
}
