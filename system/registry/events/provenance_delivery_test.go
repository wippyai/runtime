// SPDX-License-Identifier: MPL-2.0

package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
)

// provenanceRecordingListener captures the operation provenance its context
// carries at the moment each callback runs.
type provenanceRecordingListener struct {
	mu       sync.Mutex
	captured []registry.OpProvenance
	found    []bool
}

func (l *provenanceRecordingListener) record(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := registry.OpProvenanceFromContext(ctx)
	l.captured = append(l.captured, p)
	l.found = append(l.found, ok)
}

func (l *provenanceRecordingListener) Add(ctx context.Context, _ registry.Entry) error {
	l.record(ctx)
	return nil
}

func (l *provenanceRecordingListener) Update(ctx context.Context, _ registry.Entry) error {
	l.record(ctx)
	return nil
}

func (l *provenanceRecordingListener) Delete(ctx context.Context, _ registry.Entry) error {
	l.record(ctx)
	return nil
}

// TestHandlerDeliversOpProvenanceThroughTheBus pins the full delivery path:
// the dispatcher puts the pair on the event envelope, the event crosses the
// bus (where the sender's context does not survive), and the adapter injects
// the pair into the context the listener receives. A context-only carrier
// fails this test.
func TestHandlerDeliversOpProvenanceThroughTheBus(t *testing.T) {
	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
	defer cancel()

	bus := eventbus.NewBus()
	defer bus.Stop()
	ctx = event.WithBus(ctx, bus)

	listener := &provenanceRecordingListener{}
	router, err := eventbus.StartRouter(ctx, bus, eventbus.WithHandlers(NewRegistryHandler("test.*", listener)))
	require.NoError(t, err)
	defer func() { _ = router.Stop() }()

	accepted := make(chan event.Event, 1)
	_, err = bus.SubscribeP(ctx, registry.System, registry.EntryResult, accepted)
	require.NoError(t, err)

	effective := &registry.EntryProvenance{Module: "org/mod", Version: "2.0.0", Digest: "sha256:new"}
	original := &registry.EntryProvenance{Module: "org/mod", Version: "1.0.0", Digest: "sha256:old"}

	bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.EntryUpdate,
		Path:   "test:svc",
		Data: registry.Entry{
			ID:   registry.NewID("test", "svc"),
			Kind: "test.resource",
			Data: payload.NewString("data"),
		},
		Aux: registry.OpProvenance{Effective: effective, Original: original},
	})

	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("timeout waiting for the listener to acknowledge the event")
	}

	listener.mu.Lock()
	defer listener.mu.Unlock()
	require.Len(t, listener.captured, 1)
	require.True(t, listener.found[0], "the listener context must carry the operation provenance")
	got := listener.captured[0]
	require.NotNil(t, got.Effective)
	require.NotNil(t, got.Original)
	assert.Equal(t, "2.0.0", got.Effective.Version)
	assert.Equal(t, "1.0.0", got.Original.Version)
}
