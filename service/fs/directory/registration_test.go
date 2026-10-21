// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	"github.com/wippyai/runtime/system/eventbus"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

func newRegistrationHarness(t *testing.T) (context.Context, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() {
		_ = awaitSvc.Stop()
		cancel()
	})
	return event.WithAwaitService(ctx, awaitSvc), bus
}

func directoryEntry(id registry.ID) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: dirapi.Kind,
		Data: NewMockPayload(&dirapi.Config{Directory: "/tmp/test", Mode: "0755"}),
	}
}

// The next listener in the same registry transition resolves the filesystem
// as soon as Add returns, so Add must return only after the registry stores it.
func TestManager_AddReturnsAfterRegistryServesFilesystem(t *testing.T) {
	ctx, bus := newRegistrationHarness(t)
	fsReg := systemfs.NewRegistry(bus, zap.NewNop())
	require.NoError(t, fsReg.Start(ctx))
	t.Cleanup(func() { _ = fsReg.Stop() })
	transcoder := &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}
	manager := NewDirectoryManager(bus, transcoder, NewMockFactory(&MockFS{}, nil), zap.NewNop())

	for i := 0; i < 200; i++ {
		id := registry.NewID("app", "fs"+string(rune('a'+i%26))+string(rune('a'+i/26)))
		require.NoError(t, manager.Add(ctx, directoryEntry(id)))
		_, ok := fsReg.GetFS(id.String())
		require.True(t, ok, "filesystem %s must be served when Add returns", id)

		require.NoError(t, manager.Delete(ctx, directoryEntry(id)))
		_, ok = fsReg.GetFS(id.String())
		require.False(t, ok, "filesystem %s must be withdrawn when Delete returns", id)
	}
}

func TestManager_AddWaitsForRegistryAcceptAndFailsOnReject(t *testing.T) {
	ctx, bus := newRegistrationHarness(t)
	requests := make(chan event.Event, 1)
	replies := make(chan event.Kind)
	sub, err := eventbus.NewSubscriber(ctx, bus, fsapi.System, fsapi.FsRegister, func(e event.Event) {
		requests <- e
		var kind event.Kind
		select {
		case kind = <-replies:
		case <-ctx.Done():
			return
		}
		replyPath := e.Path
		if request, ok := e.Data.(fsapi.Request); ok && request.OpID != "" {
			replyPath = request.OpID
		}
		bus.Send(ctx, event.Event{System: fsapi.System, Kind: kind, Path: replyPath, Data: "test reply"})
	})
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	transcoder := &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}
	manager := NewDirectoryManager(bus, transcoder, NewMockFactory(&MockFS{}, nil), zap.NewNop())

	for _, reply := range []event.Kind{fsapi.FsAccept, fsapi.FsReject} {
		id := registry.NewID("app", "gated_"+reply)
		done := make(chan error, 1)
		go func() { done <- manager.Add(ctx, directoryEntry(id)) }()

		select {
		case <-requests:
		case <-time.After(5 * time.Second):
			t.Fatal("no registration request")
		}
		select {
		case <-done:
			t.Fatal("Add returned before the registry answered")
		case <-time.After(50 * time.Millisecond):
		}
		replies <- reply

		addErr := <-done
		manager.mu.RLock()
		_, stored := manager.directories[id]
		manager.mu.RUnlock()
		if reply == fsapi.FsAccept {
			assert.NoError(t, addErr)
			assert.True(t, stored)
		} else {
			assert.Error(t, addErr)
			assert.False(t, stored, "a rejected filesystem must not be kept")
		}
	}
}
