package process_test

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/pubsub"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
)

func TestOnCompleteAggregation(t *testing.T) {
	ctx := context.Background()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	var sum int

	cb1 := func(_ pubsub.PID, _ *runtime.Result) {
		sum++
	}

	if err := process.SetOnComplete(ctx, cb1); err != nil {
		t.Fatalf("Failed to set first onComplete: %v", err)
	}

	cb2 := func(_ pubsub.PID, _ *runtime.Result) {
		sum += 2
	}

	if err := process.SetOnComplete(ctx, cb2); err != nil {
		t.Fatalf("Failed to set second onComplete: %v", err)
	}

	onComplete := process.GetOnComplete(ctx)
	if onComplete == nil {
		t.Fatal("Expected aggregated onComplete callback, got nil")
	}

	dummyPID := pubsub.PID{
		Host:   "test",
		UniqID: "dummy",
	}
	dummyResult := &runtime.Result{}

	onComplete(dummyPID, dummyResult)

	if sum != 3 {
		t.Fatalf("Expected sum to be 3, got %d", sum)
	}
}

func TestOnStartAggregation(t *testing.T) {
	ctx := context.Background()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	sum := 0

	cb1 := func(_ pubsub.PID, _ process.Process) { sum++ }
	if err := process.SetOnStart(ctx, cb1); err != nil {
		t.Fatalf("Failed to set first onStart: %v", err)
	}

	cb2 := func(_ pubsub.PID, _ process.Process) { sum += 2 }
	if err := process.SetOnStart(ctx, cb2); err != nil {
		t.Fatalf("Failed to set second onStart: %v", err)
	}

	onStart := process.GetOnStart(ctx)
	if onStart == nil {
		t.Fatal("Expected aggregated onStart callback, got nil")
	}

	dummyPID := pubsub.PID{Host: "test", UniqID: "dummy"}
	var dummyProc process.Process

	onStart(dummyPID, dummyProc)

	if sum != 3 {
		t.Fatalf("Expected sum to be 3, got %d", sum)
	}
}

func TestNoCallbacks(t *testing.T) {
	ctx := context.Background()

	onComplete := process.GetOnComplete(ctx)
	if onComplete != nil {
		t.Error("Expected nil OnComplete, got non-nil")
	}

	onStart := process.GetOnStart(ctx)
	if onStart != nil {
		t.Error("Expected nil OnStart, got non-nil")
	}
}

func TestSingleCallbacks(t *testing.T) {
	ctx := context.Background()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	var completeCalled, startCalled bool

	cb1 := func(_ pubsub.PID, _ *runtime.Result) { completeCalled = true }
	if err := process.SetOnComplete(ctx, cb1); err != nil {
		t.Fatalf("Failed to set onComplete: %v", err)
	}

	cb2 := func(_ pubsub.PID, _ process.Process) { startCalled = true }
	if err := process.SetOnStart(ctx, cb2); err != nil {
		t.Fatalf("Failed to set onStart: %v", err)
	}

	onComplete := process.GetOnComplete(ctx)
	onStart := process.GetOnStart(ctx)

	dummyPID := pubsub.PID{Host: "test", UniqID: "dummy"}
	dummyResult := &runtime.Result{}
	var dummyProc process.Process

	onComplete(dummyPID, dummyResult)
	onStart(dummyPID, dummyProc)

	if !completeCalled {
		t.Error("OnComplete callback was not called")
	}
	if !startCalled {
		t.Error("OnStart callback was not called")
	}
}

type mockManager struct{}

func (m *mockManager) Start(ctx context.Context, _ *process.Start) (pubsub.PID, error) {
	return pubsub.PID{}, nil
}
func (m *mockManager) Terminate(context.Context, pubsub.PID) error { return nil }
func (m *mockManager) Cancel(context.Context, pubsub.PID, pubsub.PID, time.Time) error {
	return nil
}
func (m *mockManager) AttachLifecycle(ctx context.Context, _ process.Lifecycle) context.Context {
	return ctx
}

type mockPrototypeFactory struct{}

func (m *mockPrototypeFactory) Start(context.Context) error { return nil }
func (m *mockPrototypeFactory) Stop() error                 { return nil }

type mockHostRegistry struct{}

func (m *mockHostRegistry) Start(context.Context) error { return nil }
func (m *mockHostRegistry) Stop() error                 { return nil }

func TestWithManager_GetManager(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		mgr := process.GetManager(ctx)
		if mgr != nil {
			t.Error("Expected nil manager, got non-nil")
		}
	})

	t.Run("returns nil when manager not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mgr := process.GetManager(ctx)
		if mgr != nil {
			t.Error("Expected nil manager, got non-nil")
		}
	})

	t.Run("sets and retrieves manager", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockM := &mockManager{}

		ctx = process.WithManager(ctx, mockM)
		mgr := process.GetManager(ctx)

		if mgr == nil {
			t.Fatal("Expected non-nil manager")
		}
		if _, ok := mgr.(*mockManager); !ok {
			t.Error("Manager type mismatch")
		}
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockManager{}
		ctx = process.WithManager(ctx, first)

		second := &mockManager{}
		ctx = process.WithManager(ctx, second)

		mgr := process.GetManager(ctx)
		mockMgr, ok := mgr.(*mockManager)
		if !ok || mockMgr != first {
			t.Error("Expected first manager, got different one")
		}
	})

	t.Run("returns same context when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		mockM := &mockManager{}
		newCtx := process.WithManager(ctx, mockM)
		if newCtx != ctx {
			t.Error("Expected same context")
		}
	})
}

func TestWithPrototypes_GetPrototypes(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		proto := process.GetPrototypes(ctx)
		if proto != nil {
			t.Error("Expected nil prototypes, got non-nil")
		}
	})

	t.Run("returns nil when prototypes not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		proto := process.GetPrototypes(ctx)
		if proto != nil {
			t.Error("Expected nil prototypes, got non-nil")
		}
	})

	t.Run("sets and retrieves prototypes", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockP := &mockPrototypeFactory{}

		ctx = process.WithPrototypes(ctx, mockP)
		proto := process.GetPrototypes(ctx)

		if proto == nil {
			t.Fatal("Expected non-nil prototypes")
		}
		if proto != mockP {
			t.Error("Prototypes mismatch")
		}
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockPrototypeFactory{}
		ctx = process.WithPrototypes(ctx, first)

		second := &mockPrototypeFactory{}
		ctx = process.WithPrototypes(ctx, second)

		proto := process.GetPrototypes(ctx)
		if proto != first {
			t.Error("Expected first prototypes, got different one")
		}
	})
}

func TestWithHosts_GetHosts(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		hosts := process.GetHosts(ctx)
		if hosts != nil {
			t.Error("Expected nil hosts, got non-nil")
		}
	})

	t.Run("returns nil when hosts not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		hosts := process.GetHosts(ctx)
		if hosts != nil {
			t.Error("Expected nil hosts, got non-nil")
		}
	})

	t.Run("sets and retrieves hosts", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockH := &mockHostRegistry{}

		ctx = process.WithHosts(ctx, mockH)
		hosts := process.GetHosts(ctx)

		if hosts == nil {
			t.Fatal("Expected non-nil hosts")
		}
		if hosts != mockH {
			t.Error("Hosts mismatch")
		}
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockHostRegistry{}
		ctx = process.WithHosts(ctx, first)

		second := &mockHostRegistry{}
		ctx = process.WithHosts(ctx, second)

		hosts := process.GetHosts(ctx)
		if hosts != first {
			t.Error("Expected first hosts, got different one")
		}
	})
}

func TestSetOnComplete_Errors(t *testing.T) {
	t.Run("returns error when no frame context", func(t *testing.T) {
		ctx := context.Background()
		err := process.SetOnComplete(ctx, func(_ pubsub.PID, _ *runtime.Result) {})
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if err.Error() != "no frame context available" {
			t.Errorf("Expected 'no frame context available', got %s", err.Error())
		}
	})
}

func TestSetOnStart_Errors(t *testing.T) {
	t.Run("returns error when no frame context", func(t *testing.T) {
		ctx := context.Background()
		err := process.SetOnStart(ctx, func(_ pubsub.PID, _ process.Process) {})
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if err.Error() != "no frame context available" {
			t.Errorf("Expected 'no frame context available', got %s", err.Error())
		}
	})
}
