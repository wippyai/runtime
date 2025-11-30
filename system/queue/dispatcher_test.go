package queue

import (
	"context"
	"errors"
	"testing"

	queueapi "github.com/wippyai/runtime/api/dispatcher/queue"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

// mockPayload implements payload.Payload for testing
type mockPayload struct {
	data []byte
}

func (m *mockPayload) Format() payload.Format { return payload.Bytes }
func (m *mockPayload) Data() any              { return m.data }

// mockManager implements queue.Manager for testing
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

func TestPublishHandler(t *testing.T) {
	h := NewPublishHandler()
	mgr := &mockManager{}
	msg := queue.NewMessage(&mockPayload{data: []byte("test message")})

	var resp queueapi.QueuePublishResponse
	err := h.Handle(context.Background(), &queueapi.QueuePublishCmd{
		Manager: mgr,
		QueueID: registry.NewID("test", "queue1"),
		Message: msg,
	}, func(data any) {
		resp = data.(queueapi.QueuePublishResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if len(mgr.published) != 1 {
		t.Errorf("expected 1 published message, got %d", len(mgr.published))
	}
}

func TestPublishHandlerError(t *testing.T) {
	h := NewPublishHandler()
	expectedErr := errors.New("publish failed")
	mgr := &mockManager{publishErr: expectedErr}
	msg := queue.NewMessage(&mockPayload{data: []byte("test")})

	var resp queueapi.QueuePublishResponse
	err := h.Handle(context.Background(), &queueapi.QueuePublishCmd{
		Manager: mgr,
		QueueID: registry.NewID("test", "queue1"),
		Message: msg,
	}, func(data any) {
		resp = data.(queueapi.QueuePublishResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !errors.Is(resp.Error, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, resp.Error)
	}
}

func TestService(t *testing.T) {
	svc := NewService()
	if svc.Publish == nil {
		t.Error("Publish handler not initialized")
	}
}
