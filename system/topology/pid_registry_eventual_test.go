// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestPIDRegistry_RemoveReleasesEventualNames(t *testing.T) {
	svc := eventual.NewService(eventual.Config{LocalNodeID: "node-1"})
	reg := NewPIDRegistry(WithEventualRegistry(svc))
	a := pidapi.PID{Node: "node-1", Host: "host", UniqID: "a"}
	b := pidapi.PID{Node: "node-1", Host: "host", UniqID: "b"}

	_, err := svc.Register("svc.eventual", a)
	require.NoError(t, err)

	reg.Remove(a)

	res, err := svc.Lookup(context.Background(), "svc.eventual")
	require.NoError(t, err)
	assert.False(t, res.Found, "name of an exited process must not resolve")

	got, err := svc.Register("svc.eventual", b)
	require.NoError(t, err)
	assert.Equal(t, b, got)
}

func TestPIDRegistry_RemoveKeepsEventualNameOfNewerPID(t *testing.T) {
	svc := eventual.NewService(eventual.Config{LocalNodeID: "node-1"})
	reg := NewPIDRegistry(WithEventualRegistry(svc))
	a := pidapi.PID{Node: "node-1", Host: "host", UniqID: "a"}
	b := pidapi.PID{Node: "node-1", Host: "host", UniqID: "b"}

	_, err := svc.Register("svc.eventual", a)
	require.NoError(t, err)
	require.True(t, svc.Unregister("svc.eventual"))
	_, err = svc.Register("svc.eventual", b)
	require.NoError(t, err)

	reg.Remove(a)

	res, err := svc.Lookup(context.Background(), "svc.eventual")
	require.NoError(t, err)
	require.True(t, res.Found)
	assert.Equal(t, b, res.PID)
}
