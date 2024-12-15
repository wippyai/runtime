package listener

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
	"github.com/ponyruntime/pony/pkg/eventbus"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type AnotherMockPayload struct {
	Count int `json:"count"`
}

// TestOperationBus tests the EntryBus functionality.
func TestOperationBus(t *testing.T) {
	// Create a new event bus.
	bus := eventbus.NewBus(zap.NewNop())
	defer bus.Stop()

	tr := transcoder.NewTranscoder()
	tr.RegisterUnmarshaler(payload.Json, &json.ToGolang{})

	// Define test cases.
	testCases := []struct {
		name             string
		pattern          string
		mappings         []TypeMapping
		eventsToSend     []events.Event
		expectedReceived []registry.Operation
		expectedError    string
	}{
		{
			name:    "successful_subscription_and_unmarshaling",
			pattern: "component.*",
			mappings: []TypeMapping{
				WithTypeMapping("component.mock", MockPayload{}),
				WithTypeMapping("component.another", AnotherMockPayload{}),
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
				{
					System: registry.System,
					Kind:   "entry.update",
					Data: registry.Entry{
						Path: "component.config.item2",
						Kind: "component.another",
						Data: payload.NewPayload(`{"count": 42}`, payload.Json),
					},
				},
			},
			expectedReceived: []registry.Operation{
				{
					Kind: "entry.create",
					Entry: registry.Entry{
						Path: "component.config.item1",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "test_value"}`, payload.Json),
					},
					Data: &MockPayload{Value: "test_value"},
				},
				{
					Kind: "entry.update",
					Entry: registry.Entry{
						Path: "component.config.item2",
						Kind: "component.another",
						Data: payload.NewPayload(`{"count": 42}`, payload.Json),
					},
					Data: &AnotherMockPayload{Count: 42},
				},
			},
		},
		{
			name:    "no_matching_mappings",
			pattern: "component.*",
			mappings: []TypeMapping{
				WithTypeMapping("component.other", MockPayload{}),
			},
			eventsToSend: []events.Event{
				{
					System: registry.System,
					Kind:   "entry.create",
					Data: registry.Entry{
						Path: "component.config.item3",
						Kind: "component.mock",
						Data: payload.NewPayload(`{"value": "test_value"}`, payload.Json),
					},
				},
			},
			expectedReceived: []registry.Operation{}, // Expect no received events due to missing factory.
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new EntryBus for each test case.
			ob := NewOperationBus(bus, tr)

			// Context with timeout for each test case.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Subscribe to the EntryBus.
			outputCh, closeListener, err := ob.Subscribe(ctx, tc.pattern, tc.mappings...)
			if tc.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
				return
			}

			require.NoError(t, err)
			defer closeListener()

			// Send eventbus.
			for _, evt := range tc.eventsToSend {
				bus.Send(ctx, evt)
			}

			// Receive eventbus.
			var wg sync.WaitGroup
			receivedEvents := make([]registry.Operation, 0)

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
							receivedEvents = append(receivedEvents, evt)
							if len(receivedEvents) == len(tc.expectedReceived) {
								return
							}
						case <-ctx.Done():
							return // Timeout, exit.
						}
					}
				}()
			}

			// Wait for all goroutines to complete.
			wg.Wait()

			// Assertions.
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
					case *AnotherMockPayload:
						receivedData, ok := receivedEvt.Data.(*AnotherMockPayload)
						require.True(t, ok, "Data type assertion failed")
						assert.Equal(t, expectedData.Count, receivedData.Count, "AnotherMockPayload count mismatch")
					default:
						assert.Fail(t, fmt.Sprintf("Unexpected data type: %T", expectedEvt.Data))
					}
				}
			}
		})
	}
}
