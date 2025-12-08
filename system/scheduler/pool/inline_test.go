package pool

import (
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

func TestInlineBasic(t *testing.T) {
	pool, err := NewInline(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()

	pool.Start()

	result, err := pool.Call(testContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestInlineMultipleCalls(t *testing.T) {
	pool, err := NewInline(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	for i := 0; i < 100; i++ {
		_, err := pool.Call(testContext(), "test", nil)
		if err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}
}

func TestInlineFactoryError(t *testing.T) {
	_, err := NewInline(newErrorFactory(), &mockDispatcher{})
	if err == nil {
		t.Fatal("expected factory error")
	}
}

func TestInlineResultPropagation(t *testing.T) {
	factory := func() (process.Process, error) {
		return &resultProcess{result: payload.New("hello world")}, nil
	}
	pool, err := NewInline(factory, &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	result, err := pool.Call(testContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
	if result.Value == nil {
		t.Fatal("expected result.Value to be set")
	}
	if result.Value.Data() != "hello world" {
		t.Fatalf("expected 'hello world', got %v", result.Value.Data())
	}
}
