// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// registerInstance commits a registration for serviceID carrying svc, the way a
// registry transaction delivers one.
func registerInstance(h *testSupervisorHarness, serviceID string, svc supervisor.Service) {
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   serviceID,
		Data: &supervisor.Entry{
			Service: svc,
			Config: supervisor.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
			},
		},
	})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
}

// TestSupervisor_RegisterAdoptsReplacementInstance covers the handover a config
// change drives: the entry's manager rebuilds its service, and the supervisor
// must retire the instance it is supervising and adopt the replacement.
func TestSupervisor_RegisterAdoptsReplacementInstance(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:replaced"

	original := newTestService()
	registerInstance(h, serviceID, original)
	original.WaitForStart(t)
	require.True(t, original.IsStarted(), "original instance must be started")

	replacement := newTestService()
	registerInstance(h, serviceID, replacement)

	replacement.WaitForStart(t)
	require.True(t, replacement.IsStarted(), "replacement instance must be started by the supervisor")

	original.WaitForStop(t)
	require.True(t, original.IsStopped(), "superseded instance must be stopped")

	h.assertServiceState(serviceID, supervisor.StatusRunning)

	h.sup.mu.RLock()
	ctrl := h.sup.controllers[serviceID]
	h.sup.mu.RUnlock()
	require.NotNil(t, ctrl)
	require.Same(t, replacement, ctrl.Service(), "controller must supervise the replacement")
}

// TestSupervisor_RegisterKeepsIdenticalInstanceRunning guards the handover
// against needless churn: re-registering the instance already supervised must
// not stop and restart it.
func TestSupervisor_RegisterKeepsIdenticalInstanceRunning(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:stable"

	svc := newTestService()
	registerInstance(h, serviceID, svc)
	svc.WaitForStart(t)

	registerInstance(h, serviceID, svc)

	require.True(t, svc.IsStarted(), "re-registered instance must stay started")
	require.False(t, svc.IsStopped(), "re-registered instance must not be stopped")
	h.assertServiceState(serviceID, supervisor.StatusRunning)
}
