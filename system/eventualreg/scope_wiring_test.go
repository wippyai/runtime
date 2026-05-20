// SPDX-License-Identifier: MPL-2.0

package eventualreg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventualreg"
)

// TestEventualScope_RegisterUnregister exercises the EVENTUAL scope path
// the Lua glue uses to back process.registry.register(name, EVENTUAL).
// The service must store the entry locally, surface it via Lookup, and
// drop it on Unregister.
func TestEventualScope_RegisterUnregister(t *testing.T) {
	svc := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-1"})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	p := pid.PID{Node: "node-1", Host: "worker", UniqID: "abc"}
	got, err := svc.Register("session.x", p)
	require.NoError(t, err)
	assert.Equal(t, p, got)

	res, err := svc.Lookup(context.Background(), "session.x")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)

	assert.True(t, svc.Unregister("session.x"))
	res, _ = svc.Lookup(context.Background(), "session.x")
	assert.False(t, res.Found)
}

// TestEventualScope_TopologyAdapter checks that the eventualreg.Service
// implements the topology.EventualRegistry interface the Lua module uses.
func TestEventualScope_TopologyAdapter(t *testing.T) {
	svc := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-1"})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	var _ topology.EventualRegistry = svc
	_, err := svc.Lookup(context.Background(), "any", globalreg.WithFence())
	require.NoError(t, err)
}
