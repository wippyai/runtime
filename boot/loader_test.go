package boot

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap"
)

// Helper to create test context with AppContext and logger
func testContext() context.Context {
	appCtx := contextapi.NewAppContext()
	ctx := contextapi.WithAppContext(context.Background(), appCtx)
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	return ctx
}

// Mock component for testing
type mockComponent struct {
	name        string
	phase       boot.Phase
	deps        []string
	loadErr     error
	startErr    error
	stopErr     error
	loadCalled  bool
	startCalled bool
	stopCalled  bool

	LoadFn  func(context.Context) (context.Context, error)
	StartFn func(context.Context) error
	StopFn  func(context.Context) error
}

func (p *mockComponent) Name() string        { return p.name }
func (p *mockComponent) Phase() boot.Phase   { return p.phase }
func (p *mockComponent) DependsOn() []string { return p.deps }

func (p *mockComponent) Load(ctx context.Context) (context.Context, error) {
	p.loadCalled = true
	if p.LoadFn != nil {
		return p.LoadFn(ctx)
	}
	if p.loadErr != nil {
		return ctx, p.loadErr
	}
	return context.WithValue(ctx, p.name, "loaded"), nil
}

func (p *mockComponent) Start(ctx context.Context) error {
	p.startCalled = true
	if p.StartFn != nil {
		return p.StartFn(ctx)
	}
	return p.startErr
}

func (p *mockComponent) Stop(ctx context.Context) error {
	p.stopCalled = true
	if p.StopFn != nil {
		return p.StopFn(ctx)
	}
	return p.stopErr
}

func TestLoaderRegister(t *testing.T) {
	t.Run("register single component", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}
		p := &mockComponent{name: "test", phase: boot.PreInit}

		if err := loader.Register(p); err != nil {
			t.Errorf("Register() error = %v, want nil", err)
		}

		if _, exists := loader.components["test"]; !exists {
			t.Error("component not registered")
		}
	})

	t.Run("register duplicate component", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}
		p1 := &mockComponent{name: "test", phase: boot.PreInit}
		p2 := &mockComponent{name: "test", phase: boot.Init}

		loader.Register(p1)
		err = loader.Register(p2)

		if err == nil {
			t.Error("Register() expected error for duplicate, got nil")
		}
	})

	t.Run("register with dependencies", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}
		p1 := &mockComponent{name: "dep", phase: boot.PreInit}
		p2 := &mockComponent{name: "main", phase: boot.Init, deps: []string{"dep"}}

		if err := loader.Register(p1); err != nil {
			t.Errorf("Register(dep) error = %v", err)
		}
		if err := loader.Register(p2); err != nil {
			t.Errorf("Register(main) error = %v", err)
		}
	})
}

func TestLoaderLoad(t *testing.T) {
	t.Run("load single plugin", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}
		p := &mockComponent{name: "test", phase: boot.PreInit}
		loader.Register(p)

		ctx, err := loader.Load(testContext())
		if err != nil {
			t.Errorf("Load() error = %v, want nil", err)
		}

		if !p.loadCalled {
			t.Error("plugin Load() was not called")
		}

		if ctx.Value("test") != "loaded" {
			t.Error("context not updated by plugin")
		}
	})

	t.Run("load with dependencies in correct order", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		var order []string
		trackOrder := func(name string) {
			order = append(order, name)
		}

		p1 := &mockComponent{
			name:  "a",
			phase: boot.PreInit,
			LoadFn: func(ctx context.Context) (context.Context, error) {
				trackOrder("a")
				return context.WithValue(ctx, "a", "loaded"), nil
			},
		}

		p2 := &mockComponent{
			name:  "b",
			phase: boot.Init,
			deps:  []string{"a"},
			LoadFn: func(ctx context.Context) (context.Context, error) {
				trackOrder("b")
				return context.WithValue(ctx, "b", "loaded"), nil
			},
		}

		p3 := &mockComponent{
			name:  "c",
			phase: boot.Init,
			deps:  []string{"a", "b"},
			LoadFn: func(ctx context.Context) (context.Context, error) {
				trackOrder("c")
				return context.WithValue(ctx, "c", "loaded"), nil
			},
		}

		loader.Register(p1)
		loader.Register(p2)
		loader.Register(p3)

		_, err = loader.Load(testContext())
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if len(order) != 3 {
			t.Fatalf("expected 3 plugins loaded, got %d", len(order))
		}

		if order[0] != "a" {
			t.Errorf("expected 'a' first, got %q", order[0])
		}

		if order[1] != "b" {
			t.Errorf("expected 'b' second, got %q", order[1])
		}

		if order[2] != "c" {
			t.Errorf("expected 'c' third, got %q", order[2])
		}
	})

	t.Run("load with circular dependency", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		p1 := &mockComponent{name: "a", phase: boot.Init, deps: []string{"b"}}
		p2 := &mockComponent{name: "b", phase: boot.Init, deps: []string{"a"}}

		loader.Register(p1)
		loader.Register(p2)

		_, err = loader.Load(testContext())
		if err == nil {
			t.Error("Load() expected error for circular dependency, got nil")
		}
	})

	t.Run("load with missing dependency", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		p := &mockComponent{name: "main", phase: boot.Init, deps: []string{"missing"}}
		loader.Register(p)

		_, err = loader.Load(testContext())
		if err == nil {
			t.Error("Load() expected error for missing dependency, got nil")
		}
	})

	t.Run("load error propagation", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		expectedErr := errors.New("load failed")
		p := &mockComponent{name: "test", phase: boot.PreInit, loadErr: expectedErr}
		loader.Register(p)

		_, err = loader.Load(testContext())
		if err == nil {
			t.Error("Load() expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Load() error does not wrap expected error")
		}
	})
}

func TestLoaderStart(t *testing.T) {
	t.Run("start all plugins", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		p1 := &mockComponent{name: "a", phase: boot.PreInit}
		p2 := &mockComponent{name: "b", phase: boot.Init}

		loader.Register(p1)
		loader.Register(p2)

		ctx, _ := loader.Load(testContext())

		if err := loader.Start(ctx); err != nil {
			t.Errorf("boot.Start() error = %v, want nil", err)
		}

		if !p1.startCalled {
			t.Error("plugin 'a' boot.Start() not called")
		}
		if !p2.startCalled {
			t.Error("plugin 'b' boot.Start() not called")
		}
	})

	t.Run("start error propagation", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		expectedErr := errors.New("start failed")
		p := &mockComponent{name: "test", phase: boot.PreInit, startErr: expectedErr}
		loader.Register(p)

		ctx, _ := loader.Load(testContext())
		err = loader.Start(ctx)

		if err == nil {
			t.Error("boot.Start() expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("boot.Start() error does not wrap expected error")
		}
	})

	t.Run("start without load", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}
		p := &mockComponent{name: "test", phase: boot.PreInit}
		loader.Register(p)

		err = loader.Start(context.Background())
		if err != nil {
			t.Errorf("boot.Start() error = %v, want nil (no plugins loaded)", err)
		}

		if p.startCalled {
			t.Error("boot.Start() should not call plugins that weren't loaded")
		}
	})
}

func TestLoaderShutdown(t *testing.T) {
	t.Run("shutdown in reverse order", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		var order []string

		p1 := &mockComponent{
			name:  "a",
			phase: boot.PreInit,
			StopFn: func(ctx context.Context) error {
				order = append(order, "a")
				return nil
			},
		}

		p2 := &mockComponent{
			name:  "b",
			phase: boot.Init,
			deps:  []string{"a"},
			StopFn: func(ctx context.Context) error {
				order = append(order, "b")
				return nil
			},
		}

		p3 := &mockComponent{
			name:  "c",
			phase: boot.Init,
			deps:  []string{"b"},
			StopFn: func(ctx context.Context) error {
				order = append(order, "c")
				return nil
			},
		}

		loader.Register(p1)
		loader.Register(p2)
		loader.Register(p3)

		ctx, _ := loader.Load(testContext())
		loader.Shutdown(ctx)

		if len(order) != 3 {
			t.Fatalf("expected 3 plugins stopped, got %d", len(order))
		}

		if order[0] != "c" {
			t.Errorf("expected 'c' stopped first, got %q", order[0])
		}
		if order[1] != "b" {
			t.Errorf("expected 'b' stopped second, got %q", order[1])
		}
		if order[2] != "a" {
			t.Errorf("expected 'a' stopped third, got %q", order[2])
		}
	})

	t.Run("shutdown error propagation", func(t *testing.T) {
		loader, err := NewLoader(nil)
		if err != nil {
			t.Fatalf("NewLoader(nil) error = %v", err)
		}

		expectedErr := errors.New("stop failed")
		p := &mockComponent{name: "test", phase: boot.PreInit, stopErr: expectedErr}
		loader.Register(p)

		ctx, _ := loader.Load(testContext())
		err = loader.Shutdown(ctx)

		if err == nil {
			t.Error("Shutdown() expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Shutdown() error does not wrap expected error")
		}
	})
}

func TestLoaderFullLifecycle(t *testing.T) {
	loader, err := NewLoader(nil)
	if err != nil {
		t.Fatalf("NewLoader(nil) error = %v", err)
	}

	var events []string
	track := func(event string) {
		events = append(events, event)
	}

	p1 := &mockComponent{
		name:  "logger",
		phase: boot.PreInit,
		LoadFn: func(ctx context.Context) (context.Context, error) {
			track("logger:load")
			return context.WithValue(ctx, "logger", "loaded"), nil
		},
		StartFn: func(ctx context.Context) error {
			track("logger:start")
			return nil
		},
		StopFn: func(ctx context.Context) error {
			track("logger:stop")
			return nil
		},
	}

	p2 := &mockComponent{
		name:  "http",
		phase: boot.Init,
		deps:  []string{"logger"},
		LoadFn: func(ctx context.Context) (context.Context, error) {
			if ctx.Value("logger") == nil {
				return ctx, fmt.Errorf("logger not available")
			}
			track("http:load")
			return context.WithValue(ctx, "http", "loaded"), nil
		},
		StartFn: func(ctx context.Context) error {
			track("http:start")
			return nil
		},
		StopFn: func(ctx context.Context) error {
			track("http:stop")
			return nil
		},
	}

	loader.Register(p1)
	loader.Register(p2)

	ctx, err := loader.Load(testContext())
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if err := loader.Start(ctx); err != nil {
		t.Fatalf("boot.Start() failed: %v", err)
	}

	if err := loader.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	expected := []string{
		"logger:load",
		"http:load",
		"logger:start",
		"http:start",
		"http:stop",
		"logger:stop",
	}

	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(events), events)
	}

	for i, evt := range expected {
		if events[i] != evt {
			t.Errorf("event[%d] = %q, want %q", i, events[i], evt)
		}
	}
}
