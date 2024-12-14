package runner

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	eventbus "github.com/ponyruntime/pony/core/events"
)

// testComponent represents a component that can be configured via registry events.
type testComponent struct {
	mu     sync.RWMutex
	config map[registry.Path]string // Stores configuration as simple strings.
}

// newTestComponent creates a new testComponent.
func newTestComponent() *testComponent {
	return &testComponent{
		config: make(map[registry.Path]string),
	}
}

// handleEvent handles registry events and updates the component's configuration.
func (c *testComponent) handleEvent(bus events.Bus, evt events.Event) {
	if evt.System != registry.System {
		return // Ignore events from other systems.
	}

	if evt.Kind != registry.Create && evt.Kind != registry.Update {
		return
	}

	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		fmt.Printf("Received event with unexpected data type. Expected registry.Entry, got %T\n", evt.Data)
		return // Ignore events with incorrect data type.
	}

	p, ok := entry.Data.(payload.Payload)
	if !ok {
		fmt.Printf("entry.Data is not of type payload.Payload, got %T\n", entry.Data)
		return
	}

	data, ok := p.Data().(string)
	if !ok {
		fmt.Printf("payload.Data is not of type string, got %T\n", entry.Data)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.config[entry.Path] = data

	bus.Send(context.Background(), events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Data:   registry.Entry{Path: entry.Path},
	})
}

// getConfig returns the current configuration value for a given path.
func (c *testComponent) getConfig(path registry.Path) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.config[path]
	return val, ok
}

// attachComponent sets up an event listener for the testComponent.
func attachComponent(ctx context.Context, t *testing.T, bus events.Bus, component *testComponent) func() {
	// Listen for all kinds within the registry system.
	listener, err := eventbus.NewEventListener(ctx, bus, registry.System, "", component.handleEvent)
	if err != nil {
		t.Fatalf("Failed to create event listener for component: %v", err)
	}

	return func() {
		listener.Close()
	}
}

// createEntry creates registry entries with string payloads for tests.
func createEntry(path registry.Path, kind registry.Kind, data string) registry.Entry {
	return registry.Entry{
		Path: path,
		Kind: kind,
		Data: payload.NewString(data),
	}
}

func TestBusRunner_ConfigureComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := eventbus.NewBus(zap.NewNop())
	busRunner := NewBusRunner(bus, zap.NewNop())
	component := newTestComponent()
	componentClose := attachComponent(ctx, t, bus, component)
	defer componentClose()

	initialState := registry.State{}

	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/config/key1",
				"config",
				"value1",
			),
		},
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/config/key2",
				"config",
				"value2",
			),
		},
	}

	_, err := busRunner.Transition(ctx, initialState, changeSet)
	require.NoError(t, err)

	// Verify that the component received the configuration.
	val1, ok1 := component.getConfig("component/config/key1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)

	val2, ok2 := component.getConfig("component/config/key2")
	assert.True(t, ok2)
	assert.Equal(t, "value2", val2)
}
