// SPDX-License-Identifier: MPL-2.0

package eventualreg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/eventualreg"
)

// reservationCrossScope reports a single name held in a non-Eventual scope,
// standing in for the boot crossScopeChecker once a Strong reservation surfaces
// via globalreg.IsStrongReserved.
type reservationCrossScope struct {
	name string
	pid  pid.PID
}

func (c *reservationCrossScope) LookupOther(name string) (pid.PID, bool) {
	if name == c.name {
		return c.pid, true
	}
	return pid.PID{}, false
}

func (c *reservationCrossScope) NameReady() bool { return true }

// TestEventualRegister_RefusedByStrongReservation proves an EVENTUAL register of
// a name held by a Strong reservation (surfaced through the cross-scope checker)
// is refused to a different pid and allowed to the same pid.
func TestEventualRegister_RefusedByStrongReservation(t *testing.T) {
	reserved := pid.PID{Node: "node-1", Host: "h", UniqID: "owner"}
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		CrossScope:  &reservationCrossScope{name: "system.root", pid: reserved},
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	other := pid.PID{Node: "node-A", Host: "h", UniqID: "other"}
	got, err := svc.Register("system.root", other)
	require.Error(t, err)
	assert.Equal(t, eventualreg.ErrNameAlreadyRegistered, err)
	assert.Equal(t, reserved, got, "the reserved pid is surfaced as taken")

	// Same pid is not a conflict.
	got, err = svc.Register("system.root", reserved)
	require.NoError(t, err)
	assert.Equal(t, reserved, got)

	// An unreserved name registers freely.
	free := pid.PID{Node: "node-A", Host: "h", UniqID: "free"}
	got, err = svc.Register("system.other", free)
	require.NoError(t, err)
	assert.Equal(t, free, got)
}

// gatedCrossScope reports no cross-scope conflict but a toggleable barrier state.
type gatedCrossScope struct {
	ready bool
}

func (c *gatedCrossScope) LookupOther(string) (pid.PID, bool) { return pid.PID{}, false }
func (c *gatedCrossScope) NameReady() bool                    { return c.ready }

// TestEventualRegister_JoinBarrierGate proves a fresh EVENTUAL register is refused
// with ErrNameServiceNotReady while the join-epoch barrier is in progress, and
// allowed once it completes. A re-register of a held name to the same pid is
// allowed even while not ready.
func TestEventualRegister_JoinBarrierGate(t *testing.T) {
	gate := &gatedCrossScope{ready: false}
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		CrossScope:  gate,
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	p := pid.PID{Node: "node-A", Host: "h", UniqID: "p1"}
	_, err := svc.Register("evt.gated", p)
	require.ErrorIs(t, err, eventualreg.ErrNameServiceNotReady, "fresh eventual register refused while barrier in progress")

	gate.ready = true
	got, err := svc.Register("evt.gated", p)
	require.NoError(t, err)
	assert.Equal(t, p, got)

	// A re-register of the held name to the same pid is allowed even after the
	// gate re-closes.
	gate.ready = false
	got, err = svc.Register("evt.gated", p)
	require.NoError(t, err)
	assert.Equal(t, p, got)
}
