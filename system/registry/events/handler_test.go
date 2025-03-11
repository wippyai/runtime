package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEntryListener implements registry.EntryListener and registry.TransactionListener
type mockEntryListener struct {
	addCalls      []registry.Entry
	updateCalls   []registry.Entry
	deleteCalls   []registry.Entry
	beginCalled   bool
	commitCalled  bool
	discardCalled bool
	returnError   error
}

func (m *mockEntryListener) Add(_ context.Context, entry registry.Entry) error {
	m.addCalls = append(m.addCalls, entry)
	return m.returnError
}

func (m *mockEntryListener) Update(_ context.Context, entry registry.Entry) error {
	m.updateCalls = append(m.updateCalls, entry)
	return m.returnError
}

func (m *mockEntryListener) Delete(_ context.Context, entry registry.Entry) error {
	m.deleteCalls = append(m.deleteCalls, entry)
	return m.returnError
}

func (m *mockEntryListener) Begin(_ context.Context) {
	m.beginCalled = true
}

func (m *mockEntryListener) Commit(_ context.Context) {
	m.commitCalled = true
}

func (m *mockEntryListener) Discard(_ context.Context) {
	m.discardCalled = true
}

type eventCollector struct {
	mu            sync.Mutex
	acceptCalled  bool
	rejectCalled  bool
	eventReceived chan struct{}
}

func newEventCollector() *eventCollector {
	return &eventCollector{
		eventReceived: make(chan struct{}, 1),
	}
}

func (c *eventCollector) handleEvent(evt event.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch evt.Kind {
	case registry.Accept:
		c.acceptCalled = true
	case registry.Reject:
		c.rejectCalled = true
	}
	select {
	case c.eventReceived <- struct{}{}:
	default:
	}
}

func (c *eventCollector) waitForEvent(t *testing.T) {
	select {
	case <-c.eventReceived:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func (c *eventCollector) getResults() (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acceptCalled, c.rejectCalled
}

func TestNewRegistryHandler(t *testing.T) {
	tests := []struct {
		name          string
		kinds         registry.Kind
		event         event.Event
		expectAccept  bool
		expectReject  bool
		returnError   error
		expectCalls   map[string]int
		checkTxEvents bool
	}{
		{
			name:  "create event success",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Create,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
					Data: payload.NewString("test-data"),
				},
			},
			expectAccept: true,
			expectCalls:  map[string]int{"add": 1},
		},
		{
			name:  "update event success",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Update,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
					Data: payload.NewString("updated-data"),
				},
			},
			expectAccept: true,
			expectCalls:  map[string]int{"update": 1},
		},
		{
			name:  "delete event success",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Delete,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
				},
			},
			expectAccept: true,
			expectCalls:  map[string]int{"delete": 1},
		},
		{
			name:  "non-matching kind",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Create,
				Data: registry.Entry{
					ID:   registry.ID{Name: "other1"},
					Kind: "other.resource",
					Data: payload.NewString("test-data"),
				},
			},
			expectCalls: map[string]int{},
		},
		{
			name:  "create without data",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Create,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
				},
			},
			expectReject: true,
			expectCalls:  map[string]int{},
		},
		{
			name:  "operation error",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Create,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
					Data: payload.NewString("test-data"),
				},
			},
			returnError:  errors.New("operation failed"),
			expectReject: true,
			expectCalls:  map[string]int{"add": 1},
		},
		{
			name:  "skip operation",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Create,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
					Data: payload.NewString("test-data"),
				},
			},
			returnError: ErrSkipOperation,
			expectCalls: map[string]int{"add": 1},
		},
		{
			name:  "begin transaction",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Begin,
			},
			checkTxEvents: true,
			expectCalls:   map[string]int{"begin": 1},
		},
		{
			name:  "commit transaction",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Commit,
			},
			checkTxEvents: true,
			expectCalls:   map[string]int{"commit": 1},
		},
		{
			name:  "discard transaction",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Discard,
			},
			checkTxEvents: true,
			expectCalls:   map[string]int{"discard": 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			bus := eventbus.NewBus()
			listener := &mockEntryListener{returnError: tc.returnError}
			handler := NewRegistryHandler(tc.kinds, listener)

			collector := newEventCollector()
			sub, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", collector.handleEvent)
			require.NoError(t, err)
			defer sub.Close()

			err = handler.Handle(event.WithBus(ctx, bus), tc.event)
			assert.NoError(t, err)

			// Wait for any accept/reject events if expected
			if tc.expectAccept || tc.expectReject {
				collector.waitForEvent(t)
			}

			// Check accept/reject events
			acceptCalled, rejectCalled := collector.getResults()
			assert.Equal(t, tc.expectAccept, acceptCalled, "unexpected accept event")
			assert.Equal(t, tc.expectReject, rejectCalled, "unexpected reject event")

			// Verify operation calls
			if tc.checkTxEvents {
				assert.Equal(t, tc.expectCalls["begin"] > 0, listener.beginCalled, "unexpected begin call")
				assert.Equal(t, tc.expectCalls["commit"] > 0, listener.commitCalled, "unexpected commit call")
				assert.Equal(t, tc.expectCalls["discard"] > 0, listener.discardCalled, "unexpected discard call")
			} else {
				assert.Equal(t, tc.expectCalls["add"], len(listener.addCalls), "unexpected add calls")
				assert.Equal(t, tc.expectCalls["update"], len(listener.updateCalls), "unexpected update calls")
				assert.Equal(t, tc.expectCalls["delete"], len(listener.deleteCalls), "unexpected delete calls")
			}
		})
	}
}

func TestNewTransactionHandler(t *testing.T) {
	tests := []struct {
		name          string
		event         event.Event
		expectBegin   bool
		expectCommit  bool
		expectDiscard bool
	}{
		{
			name: "begin transaction",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Begin,
			},
			expectBegin: true,
		},
		{
			name: "commit transaction",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Commit,
			},
			expectCommit: true,
		},
		{
			name: "discard transaction",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Discard,
			},
			expectDiscard: true,
		},
		{
			name: "ignore non-transaction event",
			event: event.Event{
				System: registry.System,
				Kind:   registry.Create,
				Data: registry.Entry{
					ID:   registry.ID{Name: "test1"},
					Kind: "test.resource",
					Data: payload.NewString("test-data"),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			listener := &mockEntryListener{}
			handler := NewTransactionHandler(listener)

			err := handler.Handle(ctx, tc.event)
			assert.NoError(t, err)

			assert.Equal(t, tc.expectBegin, listener.beginCalled, "unexpected begin state")
			assert.Equal(t, tc.expectCommit, listener.commitCalled, "unexpected commit state")
			assert.Equal(t, tc.expectDiscard, listener.discardCalled, "unexpected discard state")
		})
	}
}
