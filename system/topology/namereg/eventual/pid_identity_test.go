// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestStateSameOwnerIgnoresStringCache(t *testing.T) {
	p := pid.PID{Node: "node", Host: "process", UniqID: "owner"}
	s := NewState(p.Node)
	require.True(t, s.Register("name", p.Precomputed(), 100, 0).Won)
	require.True(t, s.Register("name", p, 200, 0).Won)
	other := pid.PID{Node: p.Node, Host: p.Host, UniqID: "other"}
	require.False(t, s.Register("name", other, 300, 0).Won)
}

func TestServiceSameOwnerIgnoresStringCache(t *testing.T) {
	p := pid.PID{Node: "node", Host: "process", UniqID: "owner"}
	svc := NewService(Config{LocalNodeID: p.Node})
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, svc.Stop()) })

	_, err := svc.Register("name", p.Precomputed())
	require.NoError(t, err)
	before := svc.state.CVSnapshot()

	svc.reassertOwned("name")
	require.Equal(t, before, svc.state.CVSnapshot())
	require.False(t, svc.RevokeForStrong("name", p))
	resolved, found := svc.state.Lookup("name")
	require.True(t, found)
	require.True(t, resolved.Equal(p))
}
