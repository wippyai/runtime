// SPDX-License-Identifier: MPL-2.0

package host

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/internal/uniqid"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	processSystem "github.com/wippyai/runtime/system/process"
	"github.com/wippyai/runtime/system/scheduler/affinity"
	"go.uber.org/zap"
)

// --- Test Infrastructure for Manager ---

type mockCommandRegistry struct{}

func (m *mockCommandRegistry) Get(_ dispatcherapi.CommandID) dispatcherapi.Handler {
	return nil
}

func (m *mockCommandRegistry) Has(_ dispatcherapi.CommandID) bool {
	return false
}

type recordingEventBus struct {
	events []event.Event
	mu     sync.Mutex
}

func (b *recordingEventBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (b *recordingEventBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (b *recordingEventBus) Unsubscribe(context.Context, event.SubscriberID) {}

func (b *recordingEventBus) Send(_ context.Context, evt event.Event) {
	b.mu.Lock()
	b.events = append(b.events, evt)
	b.mu.Unlock()
}

func (b *recordingEventBus) reset() {
	b.mu.Lock()
	b.events = nil
	b.mu.Unlock()
}

func (b *recordingEventBus) snapshot() []event.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]event.Event(nil), b.events...)
}

func newTestManagerWithBus(_ *testing.T) (*Manager, *recordingEventBus) {
	bus := &recordingEventBus{}
	dtt := payloadSystem.GlobalTranscoder()
	json.Register(dtt)
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
	log := zap.NewNop()
	return NewManager(bus, dtt, cmdReg, factory, pidGen, log), bus
}

func newTestManager(t *testing.T) *Manager {
	mgr, _ := newTestManagerWithBus(t)
	return mgr
}

func makeHostEntry(id registry.ID) registry.Entry {
	return makeConfiguredHostEntry(id, 2, 1024, 256, nil)
}

func makeConfiguredHostEntry(id registry.ID, workers, queueSize, localQueueSize int, lifecycle map[string]any) registry.Entry {
	data := map[string]any{
		"host": map[string]any{
			"workers":          workers,
			"queue_size":       queueSize,
			"local_queue_size": localQueueSize,
		},
	}
	if lifecycle != nil {
		data["lifecycle"] = lifecycle
	}
	return registry.Entry{
		ID:   id,
		Kind: host.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(data),
	}
}

// --- Manager Construction Tests ---

func TestNewManager(t *testing.T) {
	mgr := newTestManager(t)
	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.hosts)
	assert.NotNil(t, mgr.bus)
	assert.NotNil(t, mgr.dtt)
	assert.NotNil(t, mgr.factory)
	assert.NotNil(t, mgr.pidGen)
}

// --- Manager Add Tests ---

func TestManager_Add(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.True(t, ok)
}

func TestManager_Add_DecodeError(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: host.Host,
		Meta: attrs.NewBag(),
		Data: payload.New([]byte("invalid json {")),
	}

	err := mgr.Add(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "failed to decode host config")
	assert.NotEmpty(t, hostErr.Details().GetString("cause", ""))
}

func TestManager_Add_InvalidKind(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: "invalid.kind",
		Meta: attrs.NewBag(),
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Add(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "unsupported entry kind")
	assert.Equal(t, "invalid.kind", hostErr.Details().GetString("kind", ""))
}

func TestManager_Add_MultipleHosts(t *testing.T) {
	mgr := newTestManager(t)

	entry1 := makeHostEntry(registry.NewID("test", "host1"))
	entry2 := makeHostEntry(registry.NewID("test", "host2"))

	require.NoError(t, mgr.Add(context.Background(), entry1))
	require.NoError(t, mgr.Add(context.Background(), entry2))

	mgr.mu.RLock()
	assert.Len(t, mgr.hosts, 2)
	mgr.mu.RUnlock()
}

// --- Manager Delete Tests ---

func TestManager_Delete(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	err = mgr.Delete(context.Background(), entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.False(t, ok)
}

func TestManager_Delete_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "nonexistent"),
		Kind: host.Host,
	}

	err := mgr.Delete(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: "invalid.kind",
	}

	err := mgr.Delete(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "unsupported entry kind")
	assert.Equal(t, "invalid.kind", hostErr.Details().GetString("kind", ""))
}

func TestManager_Delete_StopsHost(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	// Start the host
	mgr.mu.RLock()
	h := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	_, err = h.Start(ctxWithAppContext())
	require.NoError(t, err)

	// Delete should stop it
	err = mgr.Delete(context.Background(), entry)
	require.NoError(t, err)

	assert.False(t, h.running.Load())
}

// --- Manager Update Tests ---

func TestManager_UpdateEquivalentEffectiveConfigIsNoOp(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)
	id := registry.NewID("test", "host1")
	entry := makeConfiguredHostEntry(id, 2, 1024, 256, map[string]any{
		"requires": []any{"test:db", "test:cache"},
	})
	require.NoError(t, mgr.Add(context.Background(), entry))

	h := mgr.hosts[id]
	scheduler := h.scheduler
	config := h.cfg
	_, err := h.Start(ctxWithAppContext())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, h.Stop(context.Background())) })
	bus.reset()

	// Explicit defaults, dependency alias/order differences, and registry
	// metadata are all equivalent to the effective resident configuration.
	updated := makeConfiguredHostEntry(id, 2, 1024, 256, map[string]any{
		"depends_on": []any{"test:cache", "test:db", "test:db"},
	})
	updated.Meta = attrs.NewBagFrom(map[string]any{"title": "renamed host"})
	require.NoError(t, mgr.Update(context.Background(), updated))

	assert.Same(t, h, mgr.hosts[id])
	assert.Same(t, scheduler, h.scheduler)
	assert.Same(t, config, h.cfg, "a no-op must not publish a replacement config snapshot")
	assert.True(t, h.running.Load())
	assert.EqualValues(t, 2, h.scheduler.Stats()["workers"])
	assert.Empty(t, bus.snapshot(), "an effective no-op must emit no lifecycle or relay events")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: "invalid.kind",
		Meta: attrs.NewBag(),
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Update(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "unsupported entry kind")
	assert.Equal(t, "invalid.kind", hostErr.Details().GetString("kind", ""))
}

func TestManager_Update_NonExistent(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Update(context.Background(), entry)
	require.Error(t, err)
	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Equal(t, apierror.NotFound, hostErr.Kind())

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.False(t, ok, "an update must not resurrect an absent host")
	assert.Empty(t, bus.snapshot())
}

func TestManager_Update_DecodeError(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)

	// First add a valid host
	validEntry := makeHostEntry(registry.NewID("test", "host1"))
	require.NoError(t, mgr.Add(context.Background(), validEntry))
	h := mgr.hosts[validEntry.ID]
	config := h.cfg
	scheduler := h.scheduler
	bus.reset()

	// Then try to update with invalid data
	invalidEntry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: host.Host,
		Meta: attrs.NewBag(),
		Data: payload.New([]byte("invalid json {")),
	}

	err := mgr.Update(context.Background(), invalidEntry)
	require.Error(t, err)
	assert.Same(t, h, mgr.hosts[validEntry.ID])
	assert.Same(t, scheduler, h.scheduler)
	assert.Same(t, config, h.cfg)
	assert.Empty(t, bus.snapshot(), "decode failure must occur before live host mutation")
}

func TestManager_UpdateResizesWorkersWithoutLifecycleEvents(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)
	id := registry.NewID("test", "host1")
	require.NoError(t, mgr.Add(context.Background(), makeHostEntry(id)))
	h := mgr.hosts[id]
	scheduler := h.scheduler
	_, err := h.Start(ctxWithAppContext())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, h.Stop(context.Background())) })
	bus.reset()

	for _, workers := range []int{4, 1} {
		require.NoError(t, mgr.Update(context.Background(), makeConfiguredHostEntry(id, workers, 1024, 256, nil)))
		assert.Same(t, h, mgr.hosts[id])
		assert.Same(t, scheduler, h.scheduler)
		assert.True(t, h.running.Load())
		assert.Equal(t, workers, h.cfg.HostConfig.Workers)
		assert.EqualValues(t, workers, h.scheduler.Stats()["workers"])
	}
	assert.Empty(t, bus.snapshot(), "worker resize must not unregister or replace the host")
}

func TestManager_UpdateRejectsUnsupportedChangesAtomically(t *testing.T) {
	tests := []struct {
		name   string
		entry  func(registry.ID) registry.Entry
		fields []string
	}{
		{
			name: "global queue capacity",
			entry: func(id registry.ID) registry.Entry {
				return makeConfiguredHostEntry(id, 2, 2048, 256, nil)
			},
			fields: []string{"host.queue_size"},
		},
		{
			name: "local queue capacity",
			entry: func(id registry.ID) registry.Entry {
				return makeConfiguredHostEntry(id, 2, 1024, 512, nil)
			},
			fields: []string{"host.local_queue_size"},
		},
		{
			name: "lifecycle",
			entry: func(id registry.ID) registry.Entry {
				return makeConfiguredHostEntry(id, 2, 1024, 256, map[string]any{"auto_start": true})
			},
			fields: []string{"lifecycle"},
		},
		{
			name: "supported and unsupported fields together",
			entry: func(id registry.ID) registry.Entry {
				return makeConfiguredHostEntry(id, 4, 2048, 512, nil)
			},
			fields: []string{"host.queue_size", "host.local_queue_size"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, bus := newTestManagerWithBus(t)
			id := registry.NewID("test", "host1")
			require.NoError(t, mgr.Add(context.Background(), makeHostEntry(id)))
			h := mgr.hosts[id]
			config := h.cfg
			scheduler := h.scheduler
			bus.reset()

			err := mgr.Update(context.Background(), tt.entry(id))
			require.Error(t, err)
			var hostErr apierror.Error
			require.ErrorAs(t, err, &hostErr)
			assert.Equal(t, apierror.Conflict, hostErr.Kind())
			assert.Equal(t, tt.fields, hostErr.Details().GetSlice("fields"))
			assert.Same(t, h, mgr.hosts[id])
			assert.Same(t, scheduler, h.scheduler)
			assert.Same(t, config, h.cfg)
			assert.EqualValues(t, 2, h.scheduler.Stats()["workers"], "mixed update must not partially resize")
			assert.Empty(t, bus.snapshot())
		})
	}
}

func TestManager_UpdateRejectsWorkerChangeManagedByAffinity(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)
	mgr.SetActorAffinity(affinity.Set{0, 1, 2})
	id := registry.NewID("test", "host1")
	require.NoError(t, mgr.Add(context.Background(), makeHostEntry(id)))
	h := mgr.hosts[id]
	config := h.cfg
	bus.reset()

	err := mgr.Update(context.Background(), makeConfiguredHostEntry(id, 4, 1024, 256, nil))
	require.Error(t, err)
	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Equal(t, []string{"host.workers"}, hostErr.Details().GetSlice("fields"))
	assert.Same(t, config, h.cfg)
	assert.EqualValues(t, 3, h.scheduler.Stats()["workers"])
	assert.Empty(t, bus.snapshot())
}

func TestManager_UpdateAfterStopDoesNotPublishConfig(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)
	id := registry.NewID("test", "host1")
	require.NoError(t, mgr.Add(context.Background(), makeHostEntry(id)))
	h := mgr.hosts[id]
	config := h.cfg
	_, err := h.Start(ctxWithAppContext())
	require.NoError(t, err)
	require.NoError(t, h.Stop(context.Background()))
	bus.reset()

	err = mgr.Update(context.Background(), makeConfiguredHostEntry(id, 4, 1024, 256, nil))
	require.ErrorIs(t, err, ErrHostShuttingDown)
	assert.Same(t, config, h.cfg)
	assert.Empty(t, bus.snapshot())
}

func TestManager_UpdateDuringStopRejectsPromptly(t *testing.T) {
	mgr, bus := newTestManagerWithBus(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	mgr.factory = &mockFactory{proc: &mockProcess{stepFunc: func(_ []process.Event, out *process.StepOutput) error {
		close(entered)
		<-release
		out.Done(nil)
		return nil
	}}}

	id := registry.NewID("test", "host1")
	processCtx := process.WithLifecycleRegistry(ctxWithAppContext(), processSystem.NewLifecycleRegistry())
	require.NoError(t, mgr.Add(processCtx, makeHostEntry(id)))
	h := mgr.hosts[id]
	config := h.cfg
	_, err := h.Start(processCtx)
	require.NoError(t, err)
	_, err = h.Run(ctxWithAppContext(), &process.Start{Source: registry.NewID("test", "proc")})
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("process did not enter gated step")
	}

	stopDone := make(chan error, 1)
	stopFinished := make(chan struct{})
	go func() {
		stopDone <- h.Stop(context.Background())
		close(stopFinished)
	}()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		select {
		case <-stopFinished:
		case <-time.After(time.Second):
		}
	})
	require.Eventually(t, h.shutdown.Load, time.Second, time.Millisecond)
	assert.False(t, h.running.Load())
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned while the process step was gated: %v", err)
	default:
	}
	bus.reset()

	updateDone := make(chan error, 1)
	go func() {
		updateDone <- mgr.Update(context.Background(), makeConfiguredHostEntry(id, 4, 1024, 256, nil))
	}()
	select {
	case err := <-updateDone:
		require.ErrorIs(t, err, ErrHostShuttingDown)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Update blocked behind the scheduler drain")
	}
	assert.Same(t, config, h.cfg)
	assert.Empty(t, bus.snapshot())
	select {
	case err := <-stopDone:
		t.Fatalf("Update caused gated Stop to return: %v", err)
	default:
	}

	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-stopDone)
}

func TestManager_WorkerShrinkPublishesWithoutBlockingHostLookup(t *testing.T) {
	mgr, _ := newTestManagerWithBus(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	mgr.factory = &mockFactory{proc: &mockProcess{stepFunc: func(_ []process.Event, out *process.StepOutput) error {
		entered <- struct{}{}
		<-release
		out.Done(nil)
		return nil
	}}}
	id := registry.NewID("test", "host1")
	processCtx := process.WithLifecycleRegistry(ctxWithAppContext(), processSystem.NewLifecycleRegistry())
	require.NoError(t, mgr.Add(processCtx, makeHostEntry(id)))
	h := mgr.hosts[id]
	_, err := h.Start(processCtx)
	require.NoError(t, err)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = h.Stop(context.Background())
	})
	_, err = h.Run(ctxWithAppContext(), &process.Start{Source: registry.NewID("test", "proc")})
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter gated step")
	}

	resizeDone := make(chan error, 1)
	go func() {
		resizeDone <- mgr.Update(context.Background(), makeConfiguredHostEntry(id, 1, 1024, 256, nil))
	}()
	select {
	case err := <-resizeDone:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("shrink waited for a retiring worker")
	}
	assert.Equal(t, 1, h.cfg.HostConfig.Workers)
	assert.EqualValues(t, 1, h.scheduler.Stats()["workers"])

	lookupDone := make(chan bool, 1)
	go func() {
		got, ok := mgr.GetHost(id.String())
		lookupDone <- ok && got == h
	}()
	select {
	case ok := <-lookupDone:
		assert.True(t, ok)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("GetHost blocked behind a worker shrink")
	}

	close(release)
}

// --- Manager GetHost Tests ---

func TestManager_GetHost(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	h, ok := mgr.GetHost("test:host1")
	assert.True(t, ok)
	assert.NotNil(t, h)
}

func TestManager_GetHost_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	h, ok := mgr.GetHost("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, h)
}

func TestManager_GetHost_InvalidID(t *testing.T) {
	mgr := newTestManager(t)

	h, ok := mgr.GetHost("")
	assert.False(t, ok)
	assert.Nil(t, h)
}

// --- CompositeLifecycle Tests ---

func TestCompositeLifecycle_OnStart(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { globalCalled = true }}
	hostLC := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	require.NoError(t, err)
	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnStart_GlobalError(t *testing.T) {
	globalErr := errors.New("global lifecycle error")
	var hostCalled bool

	global := &mockLifecycle{onStartErr: globalErr}
	hostLC := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	assert.ErrorIs(t, err, globalErr)
	assert.False(t, hostCalled) // host should not be called if global fails
}

func TestCompositeLifecycle_OnStart_HostError(t *testing.T) {
	hostErr := errors.New("host lifecycle error")
	var globalCalled bool
	var rollbackResult *runtime.Result

	global := &mockLifecycle{
		onStartFunc: func(context.Context, pid.PID, process.Process) { globalCalled = true },
		onComplete:  func(_ context.Context, _ pid.PID, result *runtime.Result) { rollbackResult = result },
	}
	hostLC := &mockLifecycle{onStartErr: hostErr}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	assert.ErrorIs(t, err, hostErr)
	assert.True(t, globalCalled) // global should be called before host fails
	require.NotNil(t, rollbackResult)
	assert.ErrorIs(t, rollbackResult.Error, hostErr)
}

func TestCompositeLifecycle_OnComplete(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { globalCalled = true }}
	hostLC := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnComplete_Order(t *testing.T) {
	order := make([]string, 0, 2)

	global := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { order = append(order, "global") }}
	hostLC := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { order = append(order, "host") }}

	c := &compositeLifecycle{global: global, host: hostLC}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.Equal(t, []string{"global", "host"}, order)
}

func TestCompositeLifecycle_OnStart_Order(t *testing.T) {
	order := make([]string, 0, 2)

	global := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { order = append(order, "global") }}
	hostLC := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { order = append(order, "host") }}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"global", "host"}, order)
}

// --- Interface Compliance ---

var _ registry.EntryListener = (*Manager)(nil)

var _ dispatcherapi.Registry = (*mockCommandRegistry)(nil)
