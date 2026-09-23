// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

func TestPIDRegistrySameOwnerIgnoresStringCache(t *testing.T) {
	owner := pid.PID{Node: "node", Host: "process", UniqID: "owner"}
	cached := owner.Precomputed()
	r := NewPIDRegistry()
	_, err := r.Register("name", cached)
	require.NoError(t, err)
	_, err = r.Register("name", owner)
	require.NoError(t, err, "string caching must not change owner identity")
	for _, other := range []pid.PID{
		{Node: "other", Host: owner.Host, UniqID: owner.UniqID},
		{Node: owner.Node, Host: "other", UniqID: owner.UniqID},
		{Node: owner.Node, Host: owner.Host, UniqID: "other"},
	} {
		_, err := r.Register("name", other)
		require.ErrorIs(t, err, topologyapi.ErrNameAlreadyRegistered)
	}
}
