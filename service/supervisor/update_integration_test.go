// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	api "github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	registryevents "github.com/wippyai/runtime/system/registry/events"
	registryrunner "github.com/wippyai/runtime/system/registry/runner"
	registrytopology "github.com/wippyai/runtime/system/registry/topology"
	systemsupervisor "github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
)

type updateAttachment struct {
	ch    chan *relay.Package
	owner pid.PID
}

type updateTestNode struct {
	attachments chan updateAttachment
	cancels     chan *relay.Package
	mockNode
}

func (n *updateTestNode) Attach(owner pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	n.attachments <- updateAttachment{owner: owner, ch: ch}
	return func() {}, nil
}

func (n *updateTestNode) Send(pkg *relay.Package) error {
	n.cancels <- pkg
	return nil
}

type updateTestManager struct {
	starts       chan *processapi.Start
	releaseStart <-chan struct{}
	mockProcessManager
	next atomic.Int32
}

func (m *updateTestManager) Start(ctx context.Context, opts *processapi.Start) (pid.PID, error) {
	m.starts <- opts
	if m.releaseStart != nil {
		select {
		case <-m.releaseStart:
		case <-ctx.Done():
			return pid.PID{}, ctx.Err()
		}
	}
	return pid.PID{Node: "test-node", Host: opts.HostID, UniqID: fmt.Sprint(m.next.Add(1))}, nil
}

func receiveUpdate[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("update lifecycle did not progress")
		var zero T
		return zero
	}
}

func TestManagerUpdateReplacesRunningProcessThroughSupervisor(t *testing.T) {
	node := &updateTestNode{attachments: make(chan updateAttachment, 4), cancels: make(chan *relay.Package, 4)}
	manager := &updateTestManager{starts: make(chan *processapi.Start, 4)}
	ctx, cancel := context.WithCancel(setupTestContext(node, &mockTopology{}, manager))
	defer cancel()
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := systemsupervisor.NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(ctx))
	defer func() {
		bound, cancelStop := context.WithTimeout(context.Background(), time.Second)
		defer cancelStop()
		_ = sup.StopContext(bound)
	}()
	m := NewManager(bus, &configTranscoder{}, newTestPIDGen(), zap.NewNop())
	entry := registry.Entry{ID: registry.NewID("test", "live"), Kind: supervisorapi.ProcessService,
		Data: payload.New(supervisorapi.ServiceConfig{
			Process: registry.NewID("test", "before"), HostID: "test-host",
			Lifecycle: api.LifecycleConfig{AutoStart: true, StartTimeout: time.Second, StopTimeout: time.Second},
		})}
	bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxBegin})
	require.NoError(t, m.Begin(ctx))
	require.NoError(t, m.Add(ctx, entry))
	bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxCommit})
	require.NoError(t, m.Commit(ctx))
	first := receiveUpdate(t, node.attachments)
	start := receiveUpdate(t, manager.starts)
	require.True(t, start.Source.Equal(registry.NewID("test", "before")))
	require.Eventually(t, func() bool {
		state, err := sup.GetState(entry.ID.String())
		return err == nil && state.Status == api.StatusRunning
	}, time.Second, time.Millisecond)
	entry.Data = payload.New(supervisorapi.ServiceConfig{
		Process: registry.NewID("test", "after"), HostID: "new-host", Input: []any{"new input"},
		Lifecycle: api.LifecycleConfig{AutoStart: false, StartTimeout: time.Second, StopTimeout: time.Second},
	})
	bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxBegin})
	require.NoError(t, m.Begin(ctx))
	require.NoError(t, m.Update(ctx, entry))
	bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxCommit})
	committed := make(chan error, 1)
	go func() {
		committed <- m.Commit(ctx)
	}()
	receiveUpdate(t, node.cancels)
	first.ch <- relay.NewPackage(pid.PID{}, first.owner, topologyapi.TopicEvents,
		payload.New(&topologyapi.ExitEvent{Kind: topologyapi.Exit, Result: &runtime.Result{}}))
	require.NoError(t, receiveUpdate(t, committed))
	second := receiveUpdate(t, node.attachments)
	start = receiveUpdate(t, manager.starts)
	require.True(t, start.Source.Equal(registry.NewID("test", "after")))
	require.Equal(t, pid.HostID("new-host"), start.HostID)
	require.Equal(t, "new input", start.Input[0].Data())
	require.Eventually(t, func() bool {
		state, err := sup.GetState(entry.ID.String())
		return err == nil && state.Status == api.StatusRunning && state.Desired == api.StatusRunning
	}, time.Second, time.Millisecond, "changing AutoStart must not stop an active service")
	second.ch <- relay.NewPackage(pid.PID{}, second.owner, topologyapi.TopicEvents,
		payload.New(&topologyapi.ExitEvent{Kind: topologyapi.Exit, Result: &runtime.Result{}}))
	require.Eventually(t, func() bool {
		state, err := sup.GetState(entry.ID.String())
		return err == nil && state.Status == api.StatusExited
	}, time.Second, time.Millisecond)
}

func TestManagerRegistryTransitionWaitsForAutoStart(t *testing.T) {
	node := &updateTestNode{attachments: make(chan updateAttachment, 1), cancels: make(chan *relay.Package, 1)}
	releaseStart := make(chan struct{})
	processManager := &updateTestManager{starts: make(chan *processapi.Start, 1), releaseStart: releaseStart}
	ctx, cancel := context.WithCancel(setupTestContext(node, &mockTopology{}, processManager))
	defer cancel()

	bus := eventbus.NewBus()
	defer bus.Stop()
	ctx = event.WithBus(ctx, bus)
	awaitService := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitService.Start(ctx))
	defer func() { _ = awaitService.Stop() }()
	ctx = event.WithAwaitService(ctx, awaitService)

	sup := systemsupervisor.NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(ctx))
	defer func() {
		bound, cancelStop := context.WithTimeout(context.Background(), time.Second)
		defer cancelStop()
		_ = sup.StopContext(bound)
	}()

	manager := NewManager(bus, &configTranscoder{}, newTestPIDGen(), zap.NewNop())
	handler := registryevents.NewRegistryHandler(supervisorapi.ProcessService, manager)
	participant, ok := handler.(registry.TransactionParticipant)
	require.True(t, ok)
	kindHandler, ok := handler.(registry.KindHandler)
	require.True(t, ok)
	router, err := eventbus.StartRouter(ctx, bus, eventbus.WithHandlers(handler))
	require.NoError(t, err)
	defer func() { _ = router.Stop() }()

	busRunner := registryrunner.NewBusRunner(
		bus,
		zap.NewNop(),
		registrytopology.NewStateBuilder(zap.NewNop(), nil),
		registryrunner.WithTransactionParticipants(func() []string {
			return []string{participant.RegistryTransactionParticipantID()}
		}),
		registryrunner.WithKindHandlerCheck(kindHandler.HandlesKind),
	)
	entry := registry.Entry{
		ID:   registry.NewID("test", "pipeline"),
		Kind: supervisorapi.ProcessService,
		Data: payload.New(supervisorapi.ServiceConfig{
			Process: registry.NewID("test", "source"),
			HostID:  "test-host",
			Lifecycle: api.LifecycleConfig{
				AutoStart: true, StartTimeout: time.Second, StopTimeout: time.Second,
			},
		}),
	}

	completed := make(chan error, 1)
	go func() {
		_, err := busRunner.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}}, nil)
		completed <- err
	}()

	start := receiveUpdate(t, processManager.starts)
	require.True(t, start.Source.Equal(entry.Data.Data().(supervisorapi.ServiceConfig).Process))
	select {
	case err := <-completed:
		t.Fatalf("registry transition completed before automatic service start: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseStart)
	require.NoError(t, receiveUpdate(t, completed))
}
