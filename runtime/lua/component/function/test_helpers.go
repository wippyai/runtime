package function

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/fs"
)

type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Send(_ context.Context, e event.Event) {
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

type mockFSRegistry struct{}

func (m *mockFSRegistry) GetFS(_ string) (fs.FS, bool) {
	return nil, false
}
