package events

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	eventbus "github.com/ponyruntime/pony/pkg/eventbus"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// MockPayload is a simple struct for testing.
type MockPayload struct {
	Value string `json:"value"`
}

// TestEntryListener tests the EntryListener functionality.
func TestEntryListener(t *testing.T) {
	// Create a new event bus.
	bus := eventbus.NewBus(zap.NewNop())
	defer bus.Stop()

	tr := transcoder.NewTranscoder()
	tr.RegisterUnmarshaler(payload.Json, &json.ToGolang{})

	// Define test cases.
	testCases := []struct {
		name             string
		pattern          string
		types            map[registry.Kind]func() interface{}
		eventsToSend     []events.Event
		expectedReceived []Operation
		acceptEvents     []bool // Whether to accept or reject each event
		expectedError    string
		expectedRejects  int
	}{
		{
			name:    "successful_create",
			pattern: "component.*",
			types: map[registry.Kind]func() interface{}{
				"component.mock": func() interface{} { return &MockPayload{} },
			},
			eventsToSend: []events.Event{
				{
					System: registry.System,
					Kind:   "entry.create",
					Data: registry.Entry{
						Path: "component.config.item1",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "test_value"}`, payload.Json),
					},
				},
			},
			expectedReceived: []Operation{
				{
					Kind: "entry.create",
					Entry: registry.Entry{
						Path: "component.config.item1",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "test_value"}`, payload.Json),
					},
					Data: &MockPayload{Value: "test_value"},
				},
			},
			acceptEvents: []bool{true},
		},
		{
			name:    "successful_update",
			pattern: "component.*",
			types: map[registry.Kind]func() interface{}{
				"component.mock": func() interface{} { return &MockPayload{} },
			},
			eventsToSend: []events.Event{
				{
					System: registry.System,
					Kind:   "entry.update",
					Data: registry.Entry{
						Path: "component.config.item2",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "updated_value"}`, payload.Json),
					},
				},
			},
			expectedReceived: []Operation{
				{
					Kind: "entry.update",
					Entry: registry.Entry{
						Path: "component.config.item2",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "updated_value"}`, payload.Json),
					},
					Data: &MockPayload{Value: "updated_value"},
				},
			},
			acceptEvents: []bool{true},
		},
		{
			name:    "successful_delete",
			pattern: "component.*",
			types: map[registry.Kind]func() interface{}{
				"component.mock": func() interface{} { return &MockPayload{} },
			},
			eventsToSend: []events.Event{
				{
					System: registry.System,
					Kind:   "entry.delete",
					Data: registry.Entry{
						Path: "component.config.item3",
						Kind: "component.mock",
					},
				},
			},
			expectedReceived: []Operation{
				{
					Kind:  "entry.delete",
					Entry: registry.Entry{Path: "component.config.item3", Kind: "component.mock"},
					Data:  &MockPayload{},
				},
			},
			acceptEvents: []bool{true},
		},
		{
			name:    "pattern_mismatch",
			pattern: "component.other.*",
			types: map[registry.Kind]func() interface{}{
				"component.mock": func() interface{} { return &MockPayload{} },
			},
			eventsToSend: []events.Event{
				{
					System: registry.System,
					Kind:   "entry.create",
					Data: registry.Entry{
						Path: "component.config.item4",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "test_value"}`, payload.Json),
					},
				},
			},
			expectedReceived: []Operation{},
		},
		{
			name:    "unmarshal_error",
			pattern: "component.*",
			types: map[registry.Kind]func() interface{}{
				"component.mock": func() interface{} { return &MockPayload{} },
			},
			eventsToSend: []events.Event{
				{
					System: registry.System,
					Kind:   "entry.create",
					Data: registry.Entry{
						Path: "component.config.item5",
						Kind: "component.mock",
						Data: payload.NewPayload(`invalid_json`, payload.Json),
					},
				},
			},
			expectedReceived: []Operation{},
			expectedRejects:  1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use a WaitGroup for the entire test.
			var wg sync.WaitGroup

			// Context with timeout for each test case.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Channel for received eventbus.
			outputCh := make(chan Operation, len(tc.eventsToSend)+5)

			// Create a new EntryListener for each test case.
			listener, err := NewEntryListener(
				ctx,
				bus,
				tc.pattern,
				tc.types,
				outputCh,
				tr,
			)
			require.NoError(t, err)
			defer listener.Close()

			// Send eventbus.
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, evt := range tc.eventsToSend {
					bus.Send(ctx, evt)
				}
			}()

			// Receive eventbus.
			var receivedEventsMu sync.Mutex
			receivedEvents := make([]Operation, 0)

			if len(tc.expectedReceived) != 0 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case evt, ok := <-outputCh:
							if !ok {
								return // Channel closed, exit.
							}
							receivedEventsMu.Lock()
							receivedEvents = append(receivedEvents, evt)
							receivedEventsMu.Unlock()

							if tc.acceptEvents != nil && len(receivedEvents) <= len(tc.acceptEvents) {
								if tc.acceptEvents[len(receivedEvents)-1] {
									listener.AcceptLast()
								} else {
									listener.RejectLast(fmt.Errorf("rejected by test"))
								}
							}

							if len(receivedEvents) == len(tc.eventsToSend) {
								return
							}

							// No need for early exit, let timeout handle it.
						case <-ctx.Done():
							return // Timeout, exit.
						}
					}
				}()
			}

			// Check for rejects (if expected).
			if tc.expectedRejects > 0 {
				wg.Add(1)
				go func() {
					defer wg.Done()

					rejectCh := make(chan events.Event)
					rejectSubID, err := bus.Subscribe(ctx, registry.System, rejectCh)
					require.NoError(t, err)
					defer bus.Unsubscribe(ctx, rejectSubID)

					rejectCount := 0
					for {
						select {
						case evt, ok := <-rejectCh:
							if !ok {
								return
							}
							if evt.Kind == registry.Reject {
								rejectCount++
							}
							if rejectCount >= tc.expectedRejects {
								return
							}
						case <-ctx.Done():
							assert.Equal(t, tc.expectedRejects, rejectCount, "Number of reject eventbus mismatch")
							return
						}
					}
				}()
			}

			// Wait for all goroutines to complete.
			wg.Wait()
			cancel() // Cancel the context to cleanup resources.

			// Assertions.
			receivedEventsMu.Lock()
			if len(tc.expectedReceived) > 0 {
				require.Equal(t, len(tc.expectedReceived), len(receivedEvents), "Number of received eventbus does not match")
				for i, expectedEvt := range tc.expectedReceived {
					receivedEvt := receivedEvents[i]
					assert.Equal(t, expectedEvt.Kind, receivedEvt.Kind, "Kind mismatch")
					assert.Equal(t, expectedEvt.Entry, receivedEvt.Entry, "Entry mismatch")

					if expectedEvt.Data != nil {
						assert.Equal(t, reflect.TypeOf(expectedEvt.Data), reflect.TypeOf(receivedEvt.Data), "Data type mismatch")

						switch expectedData := expectedEvt.Data.(type) {
						case *MockPayload:
							receivedData, ok := receivedEvt.Data.(*MockPayload)
							require.True(t, ok, "Data type assertion failed")
							assert.Equal(t, expectedData.Value, receivedData.Value, "MockPayload value mismatch")
						default:
							assert.Fail(t, fmt.Sprintf("Unexpected data type: %T", expectedEvt.Data))
						}
					}
				}
			}
			receivedEventsMu.Unlock()
		})
	}
}
