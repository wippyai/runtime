// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

func TestProcessSendGrants(t *testing.T) {
	parent := pid.PID{Host: "app:host", UniqID: "parent"}
	remote := pid.PID{Node: "n2", Host: "app:host", UniqID: "parent"}
	g := NewProcessSendGrants(parent)

	require.True(t, g.Holds(parent))
	require.True(t, g.Holds(pid.PID{Host: "app:host", UniqID: "parent"}), "identity is by value")
	require.False(t, g.Holds(remote), "node is part of the identity")

	g.Grant(remote)
	require.True(t, g.Holds(remote))

	g.Grant(pid.PID{})
	require.False(t, g.Holds(pid.PID{}), "an empty PID is never granted")
}

func TestProcessSendGrantsFrame(t *testing.T) {
	child := pid.PID{Host: "app:host", UniqID: "child"}

	unrestricted, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.Nil(t, GetProcessSendGrants(unrestricted))
	GrantProcessSend(unrestricted, child)
	require.Nil(t, GetProcessSendGrants(unrestricted), "granting does not restrict a process")

	grants := NewProcessSendGrants()
	restricted, rfc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(rfc)
	require.NoError(t, rfc.SetMultiple(ProcessSendGrantsPair(grants)))
	require.Same(t, grants, GetProcessSendGrants(restricted))
	GrantProcessSend(restricted, child)
	require.True(t, grants.Holds(child))

	forked, cfc := ctxapi.ForkFrameContext(restricted)
	defer ctxapi.ReleaseFrameContext(cfc)
	inherited := GetProcessSendGrants(forked)
	require.NotNil(t, inherited, "a forked frame stays restricted")
	require.NotSame(t, grants, inherited, "a forked frame gets its own copy")
	require.True(t, inherited.Holds(child), "the copy holds what the spawner held")

	later := pid.PID{Host: "app:host", UniqID: "later"}
	inherited.Grant(later)
	require.False(t, grants.Holds(later), "acquisitions after the fork stay with the forked frame")
}

func TestProcessSendGrantsPropagateAsCopy(t *testing.T) {
	child := pid.PID{Host: "app:host", UniqID: "child"}
	grants := NewProcessSendGrants(child)
	parent, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(ProcessSendGrantsPair(grants)))

	// A spawned process starts from the propagated pairs of its spawner.
	spawned, sfc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(sfc)
	require.NoError(t, sfc.SetMultiple(ctxapi.PropagatedPairs(parent)...))

	inherited := GetProcessSendGrants(spawned)
	require.NotNil(t, inherited, "a spawned process stays restricted")
	require.NotSame(t, grants, inherited, "a spawned process gets its own copy")
	require.True(t, inherited.Holds(child))

	later := pid.PID{Host: "app:host", UniqID: "later"}
	inherited.Grant(later)
	require.False(t, grants.Holds(later), "acquisitions of the spawned process stay with it")
}
