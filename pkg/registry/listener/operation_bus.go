package listener

import (
	"context"
	"fmt"
	"reflect"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// EntryBus is a factory for creating EntryListeners.
type EntryBus struct {
	bus events.Bus
	dtt payload.Transcoder
}

// TypeMapping maps a registry Kind to a specific type for unmarshaling.
type TypeMapping struct {
	Kind      registry.Kind
	ValueType reflect.Type
}

// WithTypeMapping creates a TypeMapping for a given registry kind and type.
func WithTypeMapping(kind registry.Kind, objType any) TypeMapping {
	return TypeMapping{
		Kind:      kind,
		ValueType: reflect.TypeOf(objType),
	}
}

// NewOperationBus creates a new EntryBus.
func NewOperationBus(bus events.Bus, dtt payload.Transcoder) *EntryBus {
	return &EntryBus{
		bus: bus,
		dtt: dtt,
	}
}

// Subscribe creates a new entryListener that listens to registry entry events
// matching the given pattern. The listener will unmarshal entry data into
// types registered via TypeMapping options. Use the returned func to close the listener.
func (ob *EntryBus) Subscribe(
	ctx context.Context,
	pattern string,
	mappings ...TypeMapping,
) (chan registry.Operation, func(), error) {
	outputCh := make(chan registry.Operation)

	factories := make(map[registry.Kind]func() any)
	for _, mapping := range mappings {
		factories[mapping.Kind] = func() any {
			// Create a new object of the specified type (ValueType is already a reflect.Type).
			return reflect.New(mapping.ValueType).Interface()
		}
	}

	listener, err := newEntryListener(
		ctx,
		ob.bus,
		pattern,
		factories,
		outputCh,
		ob.dtt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create entry listener: %w", err)
	}

	return outputCh, func() { listener.Close() }, nil
}
