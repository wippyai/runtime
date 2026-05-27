// SPDX-License-Identifier: MPL-2.0

package events

import (
	"context"
	"errors"
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

// mockEntryListener implements registry.EntryListener and registry.TransactionListener
type mockEntryListener struct {
	returnError   error
	txReturnError error
	addCalls      []registry.Entry
	updateCalls   []registry.Entry
	deleteCalls   []registry.Entry
	beginCalled   bool
	commitCalled  bool
	discardCalled bool
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

func (m *mockEntryListener) Begin(_ context.Context) error {
	m.beginCalled = true
	return m.txReturnError
}

func (m *mockEntryListener) Commit(_ context.Context) error {
	m.commitCalled = true
	return m.txReturnError
}

func (m *mockEntryListener) Discard(_ context.Context) error {
	m.discardCalled = true
	return m.txReturnError
}

type eventCollector struct {
	eventReceived chan struct{}
	mu            sync.Mutex
	acceptCalled  bool
	rejectCalled  bool
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
	case registry.EntryAccept:
		c.acceptCalled = true
	case registry.EntryReject:
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
		event         event.Event
		returnError   error
		expectCalls   map[string]int
		name          string
		kinds         registry.Kind
		expectAccept  bool
		expectReject  bool
		checkTxEvents bool
	}{
		{
			name:  "create event success",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.EntryCreate,
				Data: registry.Entry{
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
				Kind:   registry.EntryUpdate,
				Data: registry.Entry{
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
				Kind:   registry.EntryDelete,
				Data: registry.Entry{
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
				Kind:   registry.EntryCreate,
				Data: registry.Entry{
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
				Kind:   registry.EntryCreate,
				Data: registry.Entry{
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
				Kind:   registry.EntryCreate,
				Data: registry.Entry{
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
				Kind:   registry.EntryCreate,
				Data: registry.Entry{
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
				Kind:   registry.TxBegin,
			},
			checkTxEvents: true,
			expectCalls:   map[string]int{"begin": 1},
		},
		{
			name:  "commit transaction",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.TxCommit,
			},
			checkTxEvents: true,
			expectCalls:   map[string]int{"commit": 1},
		},
		{
			name:  "discard transaction",
			kinds: "test.*",
			event: event.Event{
				System: registry.System,
				Kind:   registry.TxDiscard,
			},
			checkTxEvents: true,
			expectCalls:   map[string]int{"discard": 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ctxapi.NewRootContext()
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
		event         event.Event
		name          string
		expectBegin   bool
		expectCommit  bool
		expectDiscard bool
	}{
		{
			name: "begin transaction",
			event: event.Event{
				System: registry.System,
				Kind:   registry.TxBegin,
			},
			expectBegin: true,
		},
		{
			name: "commit transaction",
			event: event.Event{
				System: registry.System,
				Kind:   registry.TxCommit,
			},
			expectCommit: true,
		},
		{
			name: "discard transaction",
			event: event.Event{
				System: registry.System,
				Kind:   registry.TxDiscard,
			},
			expectDiscard: true,
		},
		{
			name: "ignore non-transaction event",
			event: event.Event{
				System: registry.System,
				Kind:   registry.EntryCreate,
				Data: registry.Entry{
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

func TestNewTransactionHandlerRejectsWhenListenerReturnsError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	bus := eventbus.NewBus()
	ctx = event.WithBus(ctx, bus)

	rejects := make(chan event.Event, 1)
	subID, err := bus.SubscribeP(ctx, registry.System, registry.TxReject, rejects)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	listener := &mockEntryListener{txReturnError: assert.AnError}
	handler := NewTransactionHandler(listener)

	err = handler.Handle(ctx, event.Event{
		System: registry.System,
		Kind:   registry.TxCommit,
		Path:   "tx/reject",
	})
	require.NoError(t, err)

	select {
	case rejection := <-rejects:
		assert.Contains(t, rejection.Path, "tx/reject/")
		assert.Equal(t, assert.AnError, rejection.Data)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transaction reject")
	}
}
