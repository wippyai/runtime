package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/require"
)

func TestSupervisor_DependencyOrdering(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-c", false, nil)                   // no deps
	h.registerServiceWithDeps("service-b", false, []string{"service-c"}) // depends on C
	h.registerServiceWithDeps("service-a", false, []string{"service-b"}) // depends on B
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// Start service A, which should trigger starting dependencies first
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Start,
		Path:   "service-a",
	})

	time.Sleep(200 * time.Millisecond)

	// Verify states
	h.assertServiceState("service-c", supervisor.Running)
	h.assertServiceState("service-b", supervisor.Running)
	h.assertServiceState("service-a", supervisor.Running)
}

func TestSupervisor_DependencyFailure(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Service B will fail to start
	svcB := h.service("service-b")
	svcB.startErr = errors.New("failed to start service B")

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-c", false, nil)
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
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
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// Start service A
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Start,
		Path:   "service-a",
	})

	time.Sleep(200 * time.Millisecond)

	// Verify states
	h.assertServiceState("service-c", supervisor.Running)
	h.assertServiceState("service-b", supervisor.Failed)  // B should fail to start
	h.assertServiceState("service-a", supervisor.Unknown) // A shouldn't start if B fails
}

func TestSupervisor_ParallelDependencyStart(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Add artificial delay to B and C
	svcB := h.service("service-b")
	svcC := h.service("service-c")
	svcB.startDelay = 200 * time.Millisecond
	svcC.startDelay = 200 * time.Millisecond

	// Register services with parallel dependencies: A -> (B,C)
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.registerServiceWithDeps("service-c", false, nil)
	h.registerServiceWithDeps("service-a", false, []string{"service-b", "service-c"})
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	startTime := time.Now()

	// Start service A
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Start,
		Path:   "service-a",
	})

	time.Sleep(300 * time.Millisecond)

	// Verify states
	h.assertServiceState("service-b", supervisor.Running)
	h.assertServiceState("service-c", supervisor.Running)
	h.assertServiceState("service-a", supervisor.Running)

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
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-c", false, nil)
	h.registerServiceWithDeps("service-b", false, []string{"service-c"})
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// Start all services by starting A
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Start,
		Path:   "service-a",
	})

	time.Sleep(200 * time.Millisecond)

	// Verify all services started
	h.assertServiceState("service-c", supervisor.Running)
	h.assertServiceState("service-b", supervisor.Running)
	h.assertServiceState("service-a", supervisor.Running)

	// Now stop service C
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Stop,
		Path:   "service-c",
	})

	time.Sleep(200 * time.Millisecond)

	// Verify all services stopped in correct order
	h.assertServiceState("service-a", supervisor.Stopped)
	h.assertServiceState("service-b", supervisor.Stopped)
	h.assertServiceState("service-c", supervisor.Stopped)
}

func TestSupervisor_MissingDependencies(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register service A with missing dependency
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.registerServiceWithDeps("service-a", false, []string{"missing-service"})
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// Try to start service A
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Start,
		Path:   "service-a",
	})

	time.Sleep(100 * time.Millisecond)

	// Verify service A didn't start
	state, err := h.sup.GetState("service-a")
	require.NoError(t, err)
	require.NotEqual(t, supervisor.Running, state.Status)
}
