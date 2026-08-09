// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	cdcsystem "github.com/wippyai/runtime/system/cdc"
	"github.com/wippyai/runtime/system/eventbus"
)

type testStream struct {
	changes chan api.Change
}

func (s *testStream) Changes() <-chan api.Change { return s.changes }
func (s *testStream) Close()                     { close(s.changes) }
func (s *testStream) Err() error                 { return nil }

type managedTestSource struct {
	info       api.SourceInfo
	startErr   error
	stopErr    error
	exclusive  string
	stream     *testStream
	startCount atomic.Int32
	stopCount  atomic.Int32
	active     atomic.Int32
	maxActive  atomic.Int32
	lifecycle  *supervisor.LifecycleConfig
}

type disposableTestSource struct {
	*managedTestSource
	disposeCount atomic.Int32
	disposeErr   error
	failedOnce   atomic.Bool
}

func (s *disposableTestSource) Dispose(ctx context.Context) error {
	s.disposeCount.Add(1)
	if s.disposeErr != nil && s.failedOnce.CompareAndSwap(false, true) {
		return s.disposeErr
	}
	return s.Stop(ctx)
}

func (s *managedTestSource) Info() api.SourceInfo { return s.info }

func (s *managedTestSource) Subscribe(context.Context, api.StreamOptions) (api.Stream, error) {
	if s.stream != nil {
		return s.stream, nil
	}
	return &testStream{changes: make(chan api.Change)}, nil
}

func (s *managedTestSource) Start(context.Context) (<-chan any, error) {
	s.startCount.Add(1)
	if s.startErr == nil {
		active := s.active.Add(1)
		for {
			max := s.maxActive.Load()
			if active <= max || s.maxActive.CompareAndSwap(max, active) {
				break
			}
		}
	}
	return nil, s.startErr
}

func (s *managedTestSource) Stop(context.Context) error {
	s.stopCount.Add(1)
	if s.active.Load() > 0 {
		s.active.Add(-1)
	}
	return s.stopErr
}

func (s *managedTestSource) LifecycleConfig() supervisor.LifecycleConfig {
	if s.lifecycle != nil {
		return *s.lifecycle
	}
	return supervisor.LifecycleConfig{AutoStart: true}
}

func (s *managedTestSource) ExclusiveResourceKey() string { return s.exclusive }

type testDriver struct {
	kind   registry.Kind
	create func(registry.Entry) (ManagedSource, error)
}

func (d testDriver) Kind() registry.Kind { return d.kind }

func (d testDriver) Create(_ context.Context, entry registry.Entry, _ Dependencies) (ManagedSource, error) {
	return d.create(entry)
}

type recordingBus struct {
	mu     sync.Mutex
	events []event.Event
}

func (b *recordingBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (b *recordingBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (*recordingBus) Unsubscribe(context.Context, event.SubscriberID) {}

func (b *recordingBus) Send(_ context.Context, e event.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
}

func (b *recordingBus) snapshot() []event.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]event.Event(nil), b.events...)
}

func newManagerTest(t *testing.T, drivers ...Driver) (*Manager, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	m, err := NewManager(cdcsystem.NewRegistry(nil), nil, bus, nil, nil, WithDriver(drivers...))
	require.NoError(t, err)
	t.Cleanup(bus.Stop)
	return m, bus
}

func TestNewManagerRequiresRegistryAndBus(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	_, err := NewManager(nil, nil, bus, nil, nil)
	require.ErrorIs(t, err, ErrRegistryRequired)
	_, err = NewManager(cdcsystem.NewRegistry(nil), nil, nil, nil, nil)
	require.ErrorIs(t, err, ErrEventBusRequired)
}

func TestManagerRoutesCanonicalIDsAndOwnsLifecycle(t *testing.T) {
	var created []*managedTestSource
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(entry registry.Entry) (ManagedSource, error) {
			source := &managedTestSource{info: api.SourceInfo{Name: "driver-name", Generation: entry.ID.String()}}
			created = append(created, source)
			return source, nil
		},
	}
	m, _ := newManagerTest(t, driver)
	id := registry.ID{NS: "app", Name: "events"}
	entry := registry.Entry{ID: id, Kind: driver.kind}

	require.NoError(t, m.Add(context.Background(), entry))
	got, ok := m.Get(registry.ParseID("app:events"))
	require.True(t, ok)
	slot, ok := got.(*sourceSlot)
	require.True(t, ok)
	slot.mu.RLock()
	require.Same(t, created[0], slot.current)
	slot.mu.RUnlock()
	require.Equal(t, "app:events", m.List()[0].ID.String())
	require.Equal(t, registry.Kind(driver.kind), m.List()[0].Kind)
	require.ErrorIs(t, m.Add(context.Background(), entry), ErrSourceExists)

	require.NoError(t, m.Delete(context.Background(), registry.Entry{ID: registry.ParseID("app:events")}))
	require.EqualValues(t, 1, created[0].stopCount.Load())
	_, ok = m.Get(id)
	require.False(t, ok)
}

func TestManagerDeleteInvokesDisposeOnlyAfterUnregister(t *testing.T) {
	source := &disposableTestSource{managedTestSource: &managedTestSource{info: api.SourceInfo{Name: "source"}}}
	driver := testDriver{
		kind:   "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) { return source, nil },
	}
	bus := &recordingBus{}
	m, err := NewManager(cdcsystem.NewRegistry(nil), nil, bus, nil, nil, WithDriver(driver))
	require.NoError(t, err)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	require.NoError(t, m.Delete(context.Background(), entry))

	require.EqualValues(t, 1, source.disposeCount.Load())
	require.EqualValues(t, 1, source.stopCount.Load())
	_, ok := m.Get(id)
	require.False(t, ok)
	events := bus.snapshot()
	require.Len(t, events, 2)
	require.Equal(t, supervisor.ServiceRegister, events[0].Kind)
	require.Equal(t, supervisor.ServiceRemove, events[1].Kind)
}

func TestManagerDeleteRetainsTombstoneForDisposeRetry(t *testing.T) {
	source := &disposableTestSource{
		managedTestSource: &managedTestSource{info: api.SourceInfo{Name: "source"}},
		disposeErr:        errors.New("cleanup failed"),
	}
	driver := testDriver{
		kind:   "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) { return source, nil },
	}
	bus := &recordingBus{}
	m, err := NewManager(cdcsystem.NewRegistry(nil), nil, bus, nil, nil, WithDriver(driver))
	require.NoError(t, err)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))

	require.EqualError(t, m.Delete(context.Background(), entry), "cleanup failed")
	_, ok := m.Get(id)
	require.True(t, ok, "failed disposal must leave a retryable tombstone")
	require.Len(t, bus.snapshot(), 1, "supervisor removal waits for successful disposal")

	require.NoError(t, m.Delete(context.Background(), entry))
	_, ok = m.Get(id)
	require.False(t, ok)
	require.EqualValues(t, 2, source.disposeCount.Load())
	require.EqualValues(t, 1, source.stopCount.Load())
	events := bus.snapshot()
	require.Len(t, events, 2)
	require.Equal(t, supervisor.ServiceRemove, events[1].Kind)
}

func TestManagerUpdateBuildFailureLeavesOldSource(t *testing.T) {
	old := &managedTestSource{info: api.SourceInfo{Name: "old"}}
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			return nil, errors.New("candidate rejected")
		},
	}
	m, _ := newManagerTest(t, driver)
	// Seed the system registry through the same manager with a temporary
	// successful driver, then switch the injected driver to the failing one.
	seed := testDriver{kind: driver.kind, create: func(registry.Entry) (ManagedSource, error) { return old, nil }}
	m.drivers[driver.kind] = seed
	id := registry.NewID("app", "events")
	require.NoError(t, m.Add(context.Background(), registry.Entry{ID: id, Kind: driver.kind}))
	m.drivers[driver.kind] = driver

	err := m.Update(context.Background(), registry.Entry{ID: id, Kind: driver.kind})
	require.EqualError(t, err, "candidate rejected")
	got, ok := m.Get(id)
	require.True(t, ok)
	slot, ok := got.(*sourceSlot)
	require.True(t, ok)
	slot.mu.RLock()
	require.Same(t, old, slot.current)
	slot.mu.RUnlock()
	require.EqualValues(t, 0, old.stopCount.Load())
}

func TestManagerUpdateRejectsEntryKindChange(t *testing.T) {
	oldKind := registry.Kind("db.cdc.old")
	newKind := registry.Kind("db.cdc.new")
	var replacementCreates atomic.Int32
	old := &managedTestSource{info: api.SourceInfo{Engine: "old"}}
	oldDriver := testDriver{
		kind:   oldKind,
		create: func(registry.Entry) (ManagedSource, error) { return old, nil },
	}
	newDriver := testDriver{
		kind: newKind,
		create: func(registry.Entry) (ManagedSource, error) {
			replacementCreates.Add(1)
			return &managedTestSource{info: api.SourceInfo{Engine: "new"}}, nil
		},
	}
	m, _ := newManagerTest(t, oldDriver)
	m.drivers[newKind] = newDriver
	id := registry.NewID("app", "events")
	require.NoError(t, m.Add(context.Background(), registry.Entry{ID: id, Kind: oldKind}))

	err := m.Update(context.Background(), registry.Entry{ID: id, Kind: newKind})
	require.ErrorIs(t, err, ErrSourceKindChange)
	require.EqualValues(t, 0, replacementCreates.Load(), "a kind-changing update must not construct another driver")
	slot := mustSlot(t, m, id)
	require.Same(t, old, slot.currentSource())
}

func TestManagerUpdateAtomicallyReplacesAndStopsOld(t *testing.T) {
	var next int
	var created []*managedTestSource
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			source := &managedTestSource{info: api.SourceInfo{Name: string(rune('a' + next))}}
			created = append(created, source)
			return source, nil
		},
	}
	m, _ := newManagerTest(t, driver)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	require.NoError(t, m.Update(context.Background(), entry))
	require.Len(t, created, 2)
	got, ok := m.Get(id)
	require.True(t, ok)
	slot, ok := got.(*sourceSlot)
	require.True(t, ok)
	slot.mu.RLock()
	require.Same(t, created[1], slot.current)
	slot.mu.RUnlock()
	require.EqualValues(t, 1, created[0].stopCount.Load())
}

func TestManagerUpdateDoesNotFailAfterNewGenerationCommits(t *testing.T) {
	old := &managedTestSource{info: api.SourceInfo{Name: "old"}, stopErr: errors.New("old cleanup failed")}
	newSource := &managedTestSource{info: api.SourceInfo{Name: "new"}}
	next := 0
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			if next == 1 {
				return old, nil
			}
			return newSource, nil
		},
	}
	m, _ := newManagerTest(t, driver)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	require.NoError(t, m.Update(context.Background(), entry))
	require.Same(t, newSource, mustSlot(t, m, id).currentSource())
	require.EqualValues(t, 1, old.stopCount.Load())
}

func TestManagerUpdateKeepsStableSupervisorRegistration(t *testing.T) {
	var next int
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			return &managedTestSource{info: api.SourceInfo{Name: string(rune('a' + next))}}, nil
		},
	}
	bus := &recordingBus{}
	m, err := NewManager(cdcsystem.NewRegistry(nil), nil, bus, nil, nil, WithDriver(driver))
	require.NoError(t, err)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	require.NoError(t, m.Update(context.Background(), entry))

	events := bus.snapshot()
	require.Len(t, events, 1)
	require.Equal(t, supervisor.ServiceRegister, events[0].Kind)
	require.Equal(t, id.String(), events[0].Path)
	registered := events[0].Data.(*supervisor.Entry)
	current, ok := m.Get(id)
	require.True(t, ok)
	require.Same(t, current, registered.Service)
}

func TestManagerUpdateReRegistersSupervisorWhenLifecycleChanges(t *testing.T) {
	oldLifecycle := supervisor.LifecycleConfig{
		AutoStart: true,
		Requires:  []string{"service-a"},
	}
	newLifecycle := supervisor.LifecycleConfig{
		AutoStart:    false,
		DependsOn:    []string{"service-b"},
		StartTimeout: 20 * time.Second,
	}
	old := &managedTestSource{
		info:      api.SourceInfo{Name: "old"},
		lifecycle: &oldLifecycle,
	}
	candidate := &managedTestSource{
		info:      api.SourceInfo{Name: "candidate"},
		lifecycle: &newLifecycle,
	}
	next := 0
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			if next == 1 {
				return old, nil
			}
			return candidate, nil
		},
	}
	bus := &recordingBus{}
	m, err := NewManager(cdcsystem.NewRegistry(nil), nil, bus, nil, nil, WithDriver(driver))
	require.NoError(t, err)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	require.NoError(t, m.Update(context.Background(), entry))

	events := bus.snapshot()
	require.Len(t, events, 3)
	require.Equal(t, supervisor.ServiceRegister, events[0].Kind)
	require.Equal(t, supervisor.ServiceRemove, events[1].Kind)
	require.Equal(t, supervisor.ServiceRegister, events[2].Kind)
	require.Equal(t, id.String(), events[1].Path)
	require.Equal(t, id.String(), events[2].Path)
	registered := events[2].Data.(*supervisor.Entry)
	require.Same(t, mustSlot(t, m, id), registered.Service)
	require.False(t, registered.Config.AutoStart)
	require.Equal(t, []string{"service-b"}, registered.Config.Requires)
	require.Empty(t, registered.Config.DependsOn)
}

func TestManagerUpdateFailedStartRetainsRunningGeneration(t *testing.T) {
	var next int
	old := &managedTestSource{info: api.SourceInfo{Name: "old"}}
	failed := &managedTestSource{info: api.SourceInfo{Name: "failed"}, startErr: errors.New("candidate start failed")}
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			if next == 1 {
				return old, nil
			}
			return failed, nil
		},
	}
	m, _ := newManagerTest(t, driver)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	source, ok := m.Get(id)
	require.True(t, ok)
	slot := source.(*sourceSlot)
	_, err := slot.Start(context.Background())
	require.NoError(t, err)

	require.EqualError(t, m.Update(context.Background(), entry), "candidate start failed")
	slot.mu.RLock()
	require.Same(t, old, slot.current)
	require.Equal(t, slotRunning, slot.state)
	slot.mu.RUnlock()
	require.EqualValues(t, 0, old.stopCount.Load())
}

func TestManagerUpdateSameExclusiveKeyStopsAndRestores(t *testing.T) {
	old := &managedTestSource{info: api.SourceInfo{Name: "old"}, exclusive: "slot-1"}
	candidate := &managedTestSource{info: api.SourceInfo{Name: "candidate"}, exclusive: "slot-1", startErr: errors.New("candidate start failed")}
	next := 0
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			if next == 1 {
				return old, nil
			}
			return candidate, nil
		},
	}
	m, _ := newManagerTest(t, driver)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	slot := mustSlot(t, m, id)
	_, err := slot.Start(context.Background())
	require.NoError(t, err)

	require.EqualError(t, m.Update(context.Background(), entry), "candidate start failed")
	slot.mu.RLock()
	require.Same(t, old, slot.current)
	require.Equal(t, slotRunning, slot.state)
	slot.mu.RUnlock()
	require.EqualValues(t, 1, old.stopCount.Load())
	require.EqualValues(t, 2, old.startCount.Load(), "old generation must be restored after its initial start")
	require.EqualValues(t, 1, old.maxActive.Load(), "exclusive generations must never overlap")
	require.Equal(t, "2", slot.Info().Generation, "restoring old ownership creates a new stream generation")
}

func TestManagerUpdateSameExclusiveKeyStopFailureFaultsSlot(t *testing.T) {
	old := &managedTestSource{info: api.SourceInfo{Name: "old"}, exclusive: "slot-1", stopErr: errors.New("old stop failed")}
	candidate := &managedTestSource{info: api.SourceInfo{Name: "candidate"}, exclusive: "slot-1"}
	next := 0
	driver := testDriver{
		kind: "db.cdc.test",
		create: func(registry.Entry) (ManagedSource, error) {
			next++
			if next == 1 {
				return old, nil
			}
			return candidate, nil
		},
	}
	m, _ := newManagerTest(t, driver)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: driver.kind}
	require.NoError(t, m.Add(context.Background(), entry))
	slot := mustSlot(t, m, id)
	_, err := slot.Start(context.Background())
	require.NoError(t, err)

	require.EqualError(t, m.Update(context.Background(), entry), "old stop failed")
	require.Same(t, old, slot.currentSource())
	slot.mu.RLock()
	require.Equal(t, slotFaulted, slot.state)
	slot.mu.RUnlock()
	require.EqualValues(t, 0, candidate.startCount.Load(), "candidate must not start while old ownership is uncertain")
}

func TestSourceSlotStampsCanonicalIdentityAndGeneration(t *testing.T) {
	id := registry.NewID("app", "events")
	upstream := &testStream{changes: make(chan api.Change, 1)}
	source := &managedTestSource{stream: upstream}
	slot := newSourceSlot(id, "db.cdc.test", source)
	_, err := slot.Start(context.Background())
	require.NoError(t, err)

	stream, err := slot.Subscribe(context.Background(), api.StreamOptions{})
	require.NoError(t, err)
	upstream.changes <- api.Change{Source: "driver-alias", Generation: "driver-generation"}

	select {
	case change := <-stream.Changes():
		require.Equal(t, id, change.SourceID)
		require.Equal(t, id.String(), change.Source)
		require.Equal(t, "1", change.Generation)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stamped change")
	}
	stream.Close()
	require.NoError(t, slot.Stop(context.Background()))
}

func TestSourceSlotInfoNormalizesLegacyAliases(t *testing.T) {
	id := registry.NewID("app", "events")
	source := &managedTestSource{info: api.SourceInfo{
		Name:       "driver-alias",
		Engine:     "driver-engine",
		Epoch:      "driver-epoch",
		Streaming:  true,
		Faulted:    true,
		DBResource: "resource:db",
	}}
	slot := newSourceSlot(id, "db.cdc.test", source)

	info := slot.Info()
	require.Equal(t, id, info.ID)
	require.Equal(t, id.String(), info.Name)
	require.Equal(t, "1", info.Generation)
	require.Equal(t, "1", info.Epoch)
	require.Equal(t, api.SourceStateUnknown, info.State)
	require.False(t, info.Streaming)
	require.False(t, info.Faulted)
	require.Equal(t, "driver-engine", info.Engine)
	require.Equal(t, "resource:db", info.DBResource)

	slot.mu.Lock()
	slot.state = slotRunning
	slot.mu.Unlock()
	info = slot.Info()
	require.Equal(t, api.SourceStateRunning, info.State)
	require.True(t, info.Streaming)
	require.False(t, info.Faulted)

	slot.mu.Lock()
	slot.state = slotFaulted
	slot.mu.Unlock()
	info = slot.Info()
	require.Equal(t, api.SourceStateFaulted, info.State)
	require.False(t, info.Streaming)
	require.True(t, info.Faulted)
}

func TestSourceSlotInfoHandlesNilSource(t *testing.T) {
	id := registry.NewID("app", "events")
	info := newSourceSlot(id, "db.cdc.test", nil).Info()
	require.Equal(t, id, info.ID)
	require.Equal(t, id.String(), info.Name)
	require.Equal(t, "1", info.Generation)
	require.Equal(t, "1", info.Epoch)
	require.Equal(t, api.SourceStateUnknown, info.State)
}

func TestSourceSlotRestartAdvancesGeneration(t *testing.T) {
	id := registry.NewID("app", "events")
	source := &managedTestSource{stream: &testStream{changes: make(chan api.Change, 1)}}
	slot := newSourceSlot(id, "db.cdc.test", source)

	_, err := slot.Start(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1", slot.Info().Generation)
	require.NoError(t, slot.Stop(context.Background()))

	_, err = slot.Start(context.Background())
	require.NoError(t, err)
	require.Equal(t, "2", slot.Info().Generation)
	stream, err := slot.Subscribe(context.Background(), api.StreamOptions{})
	require.NoError(t, err)
	source.stream.changes <- api.Change{Op: "insert"}
	select {
	case change := <-stream.Changes():
		require.Equal(t, "2", change.Generation)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for restarted stream")
	}
	stream.Close()
	require.NoError(t, slot.Stop(context.Background()))
}

func mustSlot(t *testing.T, m *Manager, id registry.ID) *sourceSlot {
	t.Helper()
	source, ok := m.Get(id)
	require.True(t, ok)
	slot, ok := source.(*sourceSlot)
	require.True(t, ok)
	return slot
}

func (s *sourceSlot) currentSource() ManagedSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func TestManagerRejectsUnsupportedAndMissingSources(t *testing.T) {
	m, _ := newManagerTest(t)
	id := registry.NewID("app", "events")
	entry := registry.Entry{ID: id, Kind: "db.cdc.unknown"}
	require.ErrorIs(t, m.Add(context.Background(), entry), ErrUnsupportedKind)
	require.ErrorIs(t, m.Update(context.Background(), entry), ErrUnsupportedKind)
	require.ErrorIs(t, m.Delete(context.Background(), entry), ErrSourceNotFound)
}

var _ event.Bus = (*eventbus.Bus)(nil)
