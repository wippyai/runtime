// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/topology/namereg/globalreg"
	"github.com/wippyai/runtime/api/pid"
)

func TestService_Lookup_NoOptions(t *testing.T) {
	svc := NewService(Config{LocalNodeID: "node-a"})
	t.Cleanup(func() { _ = svc.Stop() })

	p := makePID("node-a", "h", "p1")
	_, err := svc.Register("svc.eventual", p)
	require.NoError(t, err)

	res, err := svc.Lookup(context.Background(), "svc.eventual")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
	assert.Nil(t, res.NamesForPID)
}

func TestService_Lookup_NotFound(t *testing.T) {
	svc := NewService(Config{LocalNodeID: "node-a"})
	t.Cleanup(func() { _ = svc.Stop() })

	res, err := svc.Lookup(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, res.Found)
	assert.Equal(t, pid.PID{}, res.PID)
}

func TestService_Lookup_ByPID_Unsupported(t *testing.T) {
	svc := NewService(Config{LocalNodeID: "node-a"})
	t.Cleanup(func() { _ = svc.Stop() })

	p := makePID("node-a", "h", "p1")
	_, err := svc.Register("svc.eventual", p)
	require.NoError(t, err)

	res, err := svc.Lookup(context.Background(), "", globalreg.ByPID(p))
	require.NoError(t, err)
	assert.False(t, res.Found,
		"eventual registry has no reverse-by-PID index — ByPID returns empty without error")
	assert.Empty(t, res.NamesForPID)
}

func TestService_Lookup_Equivalence_StateLookup(t *testing.T) {
	svc := NewService(Config{LocalNodeID: "node-a"})
	t.Cleanup(func() { _ = svc.Stop() })

	p := makePID("node-a", "h", "p1")
	_, err := svc.Register("svc.parity", p)
	require.NoError(t, err)

	statePID, stateOK := svc.state.Lookup("svc.parity")
	res, err := svc.Lookup(context.Background(), "svc.parity")
	require.NoError(t, err)
	assert.Equal(t, stateOK, res.Found)
	assert.Equal(t, statePID, res.PID)
}
