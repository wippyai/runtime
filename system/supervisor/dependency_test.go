package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestSupervisor_DependencyOrdering(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-c", false, nil)                   // no deps
	h.registerServiceWithDeps("service-b", false, []string{"service-c"}) // depends on C
	h.registerServiceWithDeps("service-a", false, []string{"service-b"}) // depends on B
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Launch service A, which should trigger starting dependencies first
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait for all services to reach running state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusRunning)

	// Verify states
	h.assertServiceState("service-c", supervisor.StatusRunning)
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)
}
func TestSupervisor_DependencyFailure(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Service B will fail to start
	svcB := h.service("service-b")
	svcB.startErr = errors.New("failed to start service B")

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-c", false, nil)
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   "service-b",
		Data: &supervisor.Entry{
			Service: svcB,
			Config: supervisor.LifecycleConfig{
				DependsOn:    []string{"service-c"},
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
				RetryPolicy: supervisor.RetryPolicy{
					MaxAttempts:  3,
					InitialDelay: 100 * time.Millisecond,
				},
			},
		},
	})
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Launch service A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait a bit longer for retries and full failure
	time.Sleep(500 * time.Millisecond)

	// First verify service C is running
	h.assertServiceState("service-c", supervisor.StatusRunning)

	// Then verify B has fully failed (after retries)
	state, err := h.sup.GetState("service-b")
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusFailed, state.Status, "service-b should be in failed state")

	// Finally verify A didn't start
	h.assertServiceState("service-a", supervisor.StatusUnknown) // A shouldn't start if B fails
}

func TestSupervisor_ParallelDependencyStart(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// AddCleanup artificial delay to B and C
	svcB := h.service("service-b")
	svcC := h.service("service-c")
	svcB.startDelay = 200 * time.Millisecond
	svcC.startDelay = 200 * time.Millisecond

	// Register services with parallel dependencies: A -> (B,C)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.registerServiceWithDeps("service-c", false, nil)
	h.registerServiceWithDeps("service-a", false, []string{"service-b", "service-c"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	startTime := time.Now()

	// Launch service A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait for all services to reach running state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusRunning)

	// Verify states
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-c", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)

	// Verify parallel execution
	totalTime := time.Since(startTime)
	require.Less(t, totalTime, 400*time.Millisecond,
		"Dependencies should have started in parallel")
}

func TestSupervisor_DependencyStopOrder(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-c", false, nil)
	h.registerServiceWithDeps("service-b", false, []string{"service-c"})
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Launch all services by starting A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait for all services to reach running state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusRunning)

	// Verify all services started
	h.assertServiceState("service-c", supervisor.StatusRunning)
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)

	// Now stop service C
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-c",
	})

	// Wait for all services to reach stopped state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusStopped)

	// Verify all services stopped in correct order
	h.assertServiceState("service-a", supervisor.StatusStopped)
	h.assertServiceState("service-b", supervisor.StatusStopped)
	h.assertServiceState("service-c", supervisor.StatusStopped)
}

func TestSupervisor_MissingDependencies(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register service A with missing dependency
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-a", false, []string{"missing-service"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Try to start service A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	time.Sleep(100 * time.Millisecond)

	// Verify service A didn't start
	state, err := h.sup.GetState("service-a")
	require.NoError(t, err)
	require.NotEqual(t, supervisor.StatusRunning, state.Status)
}

func TestSupervisor_AddDependencyToExistingService(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// First register service B (not autostarted)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Wait for registration to complete and verify B is registered but not running
	time.Sleep(100 * time.Millisecond)
	state, err := h.sup.GetState("service-b")
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusUnknown, state.Status)

	// Now register service A that depends on B
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-a", true, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	time.Sleep(200 * time.Millisecond)

	// Both services should now be running because A's autostart triggered B
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)

	// close A and verify B keeps running (since it was started as dependency)
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-a",
	})

	time.Sleep(200 * time.Millisecond)

	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusStopped)
}

func TestSupervisor_ComplexDependencyChain_WithPreexisting(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// First register and start service-base
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-base", true, nil) // autostart true
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	time.Sleep(100 * time.Millisecond)
	h.assertServiceState("service-base", supervisor.StatusRunning)

	// Register service-middle that depends on service-base but don't autostart
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-middle", false, []string{"service-base"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	time.Sleep(100 * time.Millisecond)
	h.assertServiceState("service-middle", supervisor.StatusUnknown) // Should not be started

	// Now register service-top with autostart that depends on service-middle
	// This should trigger starting both service-middle and service-top
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-top", true, []string{"service-middle"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	time.Sleep(100 * time.Millisecond)

	// Verify all services are running
	h.assertServiceState("service-base", supervisor.StatusRunning)
	h.assertServiceState("service-middle", supervisor.StatusRunning)
	h.assertServiceState("service-top", supervisor.StatusRunning)

	// Now stop service-middle - this should stop service-top but leave service-base running
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-middle",
	})

	time.Sleep(100 * time.Millisecond)

	h.assertServiceState("service-base", supervisor.StatusRunning)   // Should still be running
	h.assertServiceState("service-middle", supervisor.StatusStopped) // Should be stopped
	h.assertServiceState("service-top", supervisor.StatusStopped)    // Should be stopped due to dependency

	// Finally stop service-base
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-base",
	})

	time.Sleep(100 * time.Millisecond)

	// All services should be stopped
	h.assertServiceState("service-base", supervisor.StatusStopped)
	h.assertServiceState("service-middle", supervisor.StatusStopped)
	h.assertServiceState("service-top", supervisor.StatusStopped)

	// Now try to start service-top - this should fail since dependencies are not started
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-top",
	})

	time.Sleep(100 * time.Millisecond)
	h.assertServiceState("service-base", supervisor.StatusRunning)   // Should still be running
	h.assertServiceState("service-middle", supervisor.StatusRunning) // Should still be running
	h.assertServiceState("service-top", supervisor.StatusRunning)    // Should not start due to missing deps
}

func TestSupervisor_WithoutDependencyResolver(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with only lifecycle dependencies
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Start service-a
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	h.waitForAllServices(supervisor.StatusRunning)

	// Verify lifecycle dependencies work without resolver
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)
}
