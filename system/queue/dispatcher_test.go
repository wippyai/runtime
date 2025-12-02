package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	queueapi "github.com/wippyai/runtime/api/dispatcher/queue"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

type emitFunc func(data any, err error)

func (f emitFunc) Emit(data any, err error) { f(data, err) }

type mockPayload struct {
	data []byte
}

func (m *mockPayload) Format() payload.Format { return payload.Bytes }
func (m *mockPayload) Data() any              { return m.data }

type mockManager struct {
	publishErr error
	published  []*queue.Message
}

func (m *mockManager) Publish(_ context.Context, _ registry.ID, msgs ...*queue.Message) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, msgs...)
	return nil
}

func (m *mockManager) GetDriver(_ registry.ID) (queue.Driver, bool) {
	return nil, false
}

func (m *mockManager) GetQueue(_ registry.ID) (*queue.Queue, bool) {
	return nil, false
}

func TestDispatcher_Publish(t *testing.T) {
	d := NewDispatcher(2)
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	mgr := &mockManager{}
	msg := queue.NewMessage(&mockPayload{data: []byte("test message")})

	done := make(chan queueapi.QueuePublishResponse, 1)
	err := d.handle(context.Background(), &queueapi.QueuePublishCmd{
		Manager: mgr,
		QueueID: registry.NewID("test", "queue1"),
		Message: msg,
	}, emitFunc(func(data any, _ error) {
		done <- data.(queueapi.QueuePublishResponse)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != nil {
			t.Errorf("unexpected response error: %v", resp.Error)
		}
		if len(mgr.published) != 1 {
			t.Errorf("expected 1 published message, got %d", len(mgr.published))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_PublishError(t *testing.T) {
	d := NewDispatcher(2)
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	expectedErr := errors.New("publish failed")
	mgr := &mockManager{publishErr: expectedErr}
	msg := queue.NewMessage(&mockPayload{data: []byte("test")})

	done := make(chan queueapi.QueuePublishResponse, 1)
	err := d.handle(context.Background(), &queueapi.QueuePublishCmd{
		Manager: mgr,
		QueueID: registry.NewID("test", "queue1"),
		Message: msg,
	}, emitFunc(func(data any, _ error) {
		done <- data.(queueapi.QueuePublishResponse)
	}))

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	select {
	case resp := <-done:
		if !errors.Is(resp.Error, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, resp.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_Lifecycle(t *testing.T) {
	d := NewDispatcher(4)
	if d.workers != 4 {
		t.Errorf("expected 4 workers, got %d", d.workers)
	}

	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if d.jobs == nil {
		t.Error("jobs channel not initialized")
	}

	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatcher_DefaultWorkers(t *testing.T) {
	d := NewDispatcher(0)
	if d.workers != 4 {
		t.Errorf("expected default 4 workers, got %d", d.workers)
	}
}
