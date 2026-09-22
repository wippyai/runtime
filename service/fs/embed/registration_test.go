// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/system/eventbus"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

// newAwaitContext returns a context carrying a started AwaitService over bus,
// the request/reply coordination the embed Manager uses to confirm
// registrations with the filesystem registry.
func newAwaitContext(t *testing.T, bus *eventbus.Bus) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	ctx = event.WithAwaitService(ctx, awaitSvc)
	t.Cleanup(func() {
		_ = awaitSvc.Stop()
		cancel()
	})
	return ctx
}

// newFSRegistryHarness starts the filesystem registry the embed Manager
// registers with, on its own bus.
func newFSRegistryHarness(t *testing.T) (context.Context, *eventbus.Bus, *systemfs.Registry) {
	t.Helper()
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx := newAwaitContext(t, bus)
	fsReg := systemfs.NewRegistry(bus, zap.NewNop())
	require.NoError(t, fsReg.Start(ctx))
	t.Cleanup(func() { _ = fsReg.Stop() })
	return ctx, bus, fsReg
}

// gatedFSRegistry answers fs.register the way the filesystem registry does,
// but only once the test releases it, so a test can observe whether a caller
// returns before its registration is accepted.
type gatedFSRegistry struct {
	registered chan event.Event
	release    chan event.Kind
}

func newGatedFSRegistry(t *testing.T, ctx context.Context, bus *eventbus.Bus) *gatedFSRegistry {
	t.Helper()
	g := &gatedFSRegistry{
		registered: make(chan event.Event, 1),
		release:    make(chan event.Kind),
	}
	stopped := make(chan struct{})
	sub, err := eventbus.NewSubscriber(ctx, bus, fsapi.System, fsapi.FsRegister, func(e event.Event) {
		select {
		case g.registered <- e:
		case <-stopped:
			return
		}
		select {
		case reply := <-g.release:
			bus.Send(ctx, event.Event{System: fsapi.System, Kind: reply, Path: e.Path, Data: "gated reply"})
		case <-stopped:
		}
	})
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	t.Cleanup(func() { close(stopped) })
	return g
}

func (g *gatedFSRegistry) awaitRegistration(t *testing.T) event.Event {
	t.Helper()
	select {
	case e := <-g.registered:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("fs.register was not published")
		return event.Event{}
	}
}

func embedEntry(ns, name string) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID(ns, name),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}
}

// assertBlockedUntilAccepted runs op, which must publish fs.register and must
// not return until the registry replies. It then releases the reply and
// returns op's result.
func assertBlockedUntilAccepted(t *testing.T, gate *gatedFSRegistry, reply event.Kind, op func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- op() }()

	gate.awaitRegistration(t)
	select {
	case err := <-done:
		t.Fatalf("returned before the filesystem registry answered the registration (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
	}

	gate.release <- reply
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("did not return after the filesystem registry answered")
		return nil
	}
}

func TestManager_AddReturnsOnlyAfterFilesystemRegistryAccepts(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx := newAwaitContext(t, bus)
	gate := newGatedFSRegistry(t, ctx, bus)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"}), nil))
	manager := NewManager(bus, &mockDTT{}, reg, zap.NewNop())
	entry := embedEntry("ui", "app")

	err := assertBlockedUntilAccepted(t, gate, fsapi.FsAccept, func() error { return manager.Add(ctx, entry) })
	require.NoError(t, err)
	assert.Equal(t, "1", readManagerFile(t, manager, entry.ID, "v.txt"))
}

func TestManager_UpdateReturnsOnlyAfterFilesystemRegistryAccepts(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx := newAwaitContext(t, bus)
	gate := newGatedFSRegistry(t, ctx, bus)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"}), nil))
	manager := NewManager(bus, &mockDTT{}, reg, zap.NewNop())
	entry := embedEntry("ui", "app")
	require.NoError(t, assertBlockedUntilAccepted(t, gate, fsapi.FsAccept, func() error { return manager.Add(ctx, entry) }))

	require.NoError(t, reg.StagePack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "2"}), nil))
	err := assertBlockedUntilAccepted(t, gate, fsapi.FsAccept, func() error { return manager.Update(ctx, entry) })
	require.NoError(t, err)
	assert.Equal(t, "2", readManagerFile(t, manager, entry.ID, "v.txt"))
}

func TestManager_RejectedRegistrationFailsAndKeepsCurrentFilesystem(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx := newAwaitContext(t, bus)
	gate := newGatedFSRegistry(t, ctx, bus)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"}), nil))
	manager := NewManager(bus, &mockDTT{}, reg, zap.NewNop())
	entry := embedEntry("ui", "app")

	err := assertBlockedUntilAccepted(t, gate, fsapi.FsReject, func() error { return manager.Add(ctx, entry) })
	require.Error(t, err)
	manager.mu.RLock()
	_, stored := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	assert.False(t, stored, "a rejected registration must not be recorded as served")

	require.NoError(t, assertBlockedUntilAccepted(t, gate, fsapi.FsAccept, func() error { return manager.Add(ctx, entry) }))
	require.NoError(t, reg.StagePack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "2"}), nil))
	err = assertBlockedUntilAccepted(t, gate, fsapi.FsReject, func() error { return manager.Update(ctx, entry) })
	require.Error(t, err)
	assert.Equal(t, "1", readManagerFile(t, manager, entry.ID, "v.txt"),
		"a rejected update must keep the filesystem the registry still serves")
}

// The filesystem registry serves the handle a listener registered as soon as
// the listener returns: the next registry listener in the same transition
// resolves the filesystem through it.
func TestManager_FilesystemRegistryServesHandleWhenManagerReturns(t *testing.T) {
	ctx, bus, fsReg := newFSRegistryHarness(t)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterPack("org/mod-v0.wapp", "org/mod", "0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "0"}), nil))
	manager := NewManager(bus, &mockDTT{}, reg, zap.NewNop())
	entry := embedEntry("ui", "app")
	require.NoError(t, manager.Add(ctx, entry))
	assert.Equal(t, "0", readRegistryFile(t, fsReg, entry.ID, "v.txt"))

	for i := 1; i <= 50; i++ {
		version := strconv.Itoa(i)
		path := "org/mod-v" + version + ".wapp"
		require.NoError(t, reg.StagePack(path, "org/mod", version,
			createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": version}), nil))
		require.NoError(t, manager.Update(ctx, entry))
		require.Equal(t, version, readRegistryFile(t, fsReg, entry.ID, "v.txt"),
			"iteration %d: the filesystem registry must serve the handle Update issued", i)
		require.NoError(t, reg.ActivatePack(path))
	}
}

func readRegistryFile(t *testing.T, fsReg *systemfs.Registry, id registry.ID, name string) string {
	t.Helper()
	fsys, ok := fsReg.GetFS(id.String())
	require.True(t, ok, "filesystem registry has no handle for %s", id)
	file, err := fsys.Open(name)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	return string(data)
}

// recordingAwaitService records the timeout each wait is prepared with and
// delegates to a real AwaitService.
type recordingAwaitService struct {
	event.AwaitService
	timeouts []time.Duration
}

func (s *recordingAwaitService) Prepare(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) (event.AwaitWaiter, error) {
	s.timeouts = append(s.timeouts, timeout)
	return s.AwaitService.Prepare(ctx, system, kind, path, timeout)
}

// A registration must complete however long the filesystem registry takes to
// answer: the wait is bounded by the operation context alone, never by a
// fixed budget such as event.DefaultAwaitTimeout.
func TestManager_RegistrationWaitIsBoundOnlyByContext(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	t.Cleanup(cancel)
	inner := eventbus.NewAwaitService(bus)
	require.NoError(t, inner.Start(ctx))
	t.Cleanup(func() { _ = inner.Stop() })
	recorder := &recordingAwaitService{AwaitService: inner}
	ctx = event.WithAwaitService(ctx, recorder)
	gate := newGatedFSRegistry(t, ctx, bus)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"}), nil))
	manager := NewManager(bus, &mockDTT{}, reg, zap.NewNop())
	entry := embedEntry("ui", "app")
	require.NoError(t, assertBlockedUntilAccepted(t, gate, fsapi.FsAccept, func() error { return manager.Add(ctx, entry) }))
	require.Equal(t, []time.Duration{event.ContextBoundAwait}, recorder.timeouts)

	// Canceling the operation context is what ends a pending wait.
	opCtx, opCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- manager.Update(opCtx, entry) }()
	gate.awaitRegistration(t)
	select {
	case err := <-done:
		t.Fatalf("Update returned before the registry answered or the context ended: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	opCancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Update did not return after its context was canceled")
	}
}

// Without a filesystem registry subscribed, nothing can ever answer, so the
// operation fails immediately instead of waiting.
func TestManager_FailsImmediatelyWithoutFilesystemRegistry(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx := newAwaitContext(t, bus)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0",
		createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"}), nil))
	manager := NewManager(bus, &mockDTT{}, reg, zap.NewNop())

	done := make(chan error, 1)
	go func() { done <- manager.Add(ctx, embedEntry("ui", "app")) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, systemfs.ErrRegistrationCoordinationUnavailable)
	case <-time.After(5 * time.Second):
		t.Fatal("Add blocked with no filesystem registry subscribed")
	}
}
