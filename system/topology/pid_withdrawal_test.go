// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

func TestWithdrawLocalRetainsParentAndSealsRegistration(t *testing.T) {
	parent := NewPIDRegistry()
	remote := pid.PID{Node: "remote", Host: "host", UniqID: "p"}
	_, err := parent.Register("parent", remote)
	require.NoError(t, err)
	r := NewPIDRegistry(WithNameGuard(&topologyapi.NameGuard{}), WithParent(parent))
	local := pid.PID{Node: "local", Host: "host", UniqID: "p"}
	_, err = r.Register("local", local)
	require.NoError(t, err)
	require.NoError(t, r.WithdrawLocal(context.Background()))
	_, found := r.LookupLocal("local")
	require.False(t, found)
	got, found := r.Lookup("parent")
	require.True(t, found)
	require.True(t, got.Equal(remote))
	_, err = r.Register("late", local)
	require.ErrorIs(t, err, topologyapi.ErrNameAdmissionClosed)
	require.NoError(t, r.WithdrawLocal(context.Background()))
}
