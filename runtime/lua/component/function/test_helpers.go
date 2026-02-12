package function

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

type mockEventBus struct {
	acceptChan   chan struct{}
	events       []event.Event
	onSend       func()
	shouldAccept bool
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		acceptChan:   make(chan struct{}),
		shouldAccept: true,
	}
}

func (m *mockEventBus) Send(_ context.Context, e event.Event) {
	if m.onSend != nil {
		m.onSend()
	}
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

type mockDispatcher struct{}

func (m *mockDispatcher) Dispatch(_ dispatcher.Command) dispatcher.Handler {
	return nil
}

type mockFSRegistry struct {
	files map[string][]byte
}

func newMockFSRegistry() *mockFSRegistry {
	return &mockFSRegistry{
		files: make(map[string][]byte),
	}
}

func (m *mockFSRegistry) GetFS(_ string) (fs.FS, bool) {
	return nil, false
}

type mockCompiledFactory struct {
	shouldFail bool
	callCount  int
}

func newMockCompiledFactory() *mockCompiledFactory {
	return &mockCompiledFactory{}
}

func (m *mockCompiledFactory) CreateFactory(_ registry.ID, _ ...engine.FactoryOption) (process.FactoryFunc, error) {
	m.callCount++
	if m.shouldFail {
		return nil, &mockError{msg: "mock compile error"}
	}
	return func() (process.Process, error) {
		return &mockProcess{}, nil
	}, nil
}

type mockProcess struct{}

func (m *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step(_ []process.Event, _ *process.StepOutput) error {
	return nil
}

func (m *mockProcess) Close() {}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
