// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	sysprocess "github.com/wippyai/runtime/system/process"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	syssupervisor "github.com/wippyai/runtime/system/supervisor"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

// scheduledServiceManager launches real actors so the test covers step outcome,
// topology EXIT delivery, process-service monitoring and controller retry policy.
type scheduledServiceManager struct {
	sched   *actor.Scheduler
	pidGen  processapi.PIDGenerator
	entered chan pid.PID
}

func (m *scheduledServiceManager) Start(ctx context.Context, start *processapi.Start) (pid.PID, error) {
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	if err := fc.Set(runtime.FrameLifecycleOptionsKey, start.Options); err != nil {
		return pid.PID{}, err
	}
	id := m.pidGen.Generate(start.HostID)
	p := &terminatedServiceProcess{id: id, entered: m.entered}
	_, err := m.sched.Submit(ctx, id, p, "", nil)
	return id, err
}

func (m *scheduledServiceManager) Terminate(_ context.Context, id pid.PID) error {
	return m.sched.Terminate(id)
}
func (*scheduledServiceManager) Cancel(context.Context, pid.PID, pid.PID, string) error {
	return nil
}

type terminatedServiceProcess struct {
	ctx     context.Context
	entered chan pid.PID
	id      pid.PID
}

func (p *terminatedServiceProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}
func (p *terminatedServiceProcess) Step(_ []processapi.Event, out *processapi.StepOutput) error {
	p.entered <- p.id
	<-p.ctx.Done()
	out.Done(nil)
	return nil
}
func (*terminatedServiceProcess) Close() {}

func TestTerminatedExecutingActorRestartsProcessService(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := sysrelay.NewNode("test-node")
	router := sysrelay.NewRouter(node, nil)
	topo := systopology.NewTopology(router, node.ID())
	sched := actor.NewScheduler(scheduler.NewRegistry(), actor.WithWorkers(1),
		actor.WithLifecycle(systopology.NewLifecycle(topo, nil, zap.NewNop())))
	sched.Start()
	defer func() {
		cancel()
		ctx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		sched.Stop(ctx)
	}()
	require.NoError(t, node.RegisterHost("test-host", sched))
	require.NoError(t, node.RegisterHost(topologyapi.ControlHost, sysrelay.NewMailbox(root)))
	manager := &scheduledServiceManager{
		sched: sched, pidGen: uniqid.NewPIDGenerator(uniqid.NewGenerator(), node.ID()),
		entered: make(chan pid.PID, 4),
	}
	ctx := setupTestContext(node, topo, manager)
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	svc := newTestService()
	config := supervisor.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: time.Second, StableThreshold: time.Minute,
		RetryPolicy: supervisor.RetryPolicy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxAttempts: 3},
	}
	failures := make(chan error, 4)
	controller := syssupervisor.NewController(ctx, svc, config, func(_ supervisor.Status, details any) {
		if err, ok := details.(error); ok {
			failures <- err
		}
	})
	require.NoError(t, controller.Start())
	var first pid.PID
	select {
	case first = <-manager.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("initial service did not enter its actor step")
	}
	require.NoError(t, manager.Terminate(ctx, first))
	select {
	case err := <-failures:
		require.ErrorIs(t, err, sysprocess.ErrTerminated)
	case <-time.After(5 * time.Second):
		t.Fatal("service did not report termination as a failure")
	}
	select {
	case second := <-manager.entered:
		require.NotEqual(t, first, second, "restart must create a new process")
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not restart the terminated actor")
	}
}

var _ processapi.Manager = (*scheduledServiceManager)(nil)
var _ relay.Receiver = (*actor.Scheduler)(nil)
