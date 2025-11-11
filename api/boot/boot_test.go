package boot

import (
	"context"
	"errors"
	"testing"
)

// Mock plugin for testing
type mockPlugin struct {
	name        string
	phase       Phase
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

func (p *mockPlugin) Name() string        { return p.name }
func (p *mockPlugin) Phase() Phase        { return p.phase }
func (p *mockPlugin) DependsOn() []string { return p.deps }

func (p *mockPlugin) Load(ctx context.Context) (context.Context, error) {
	p.loadCalled = true
	if p.LoadFn != nil {
		return p.LoadFn(ctx)
	}
	if p.loadErr != nil {
		return ctx, p.loadErr
	}
	return context.WithValue(ctx, p.name, "loaded"), nil
}

func (p *mockPlugin) Start(ctx context.Context) error {
	p.startCalled = true
	if p.StartFn != nil {
		return p.StartFn(ctx)
	}
	return p.startErr
}

func (p *mockPlugin) Stop(ctx context.Context) error {
	p.stopCalled = true
	if p.StopFn != nil {
		return p.StopFn(ctx)
	}
	return p.stopErr
}

func TestPhaseString(t *testing.T) {
	tests := []struct {
		phase Phase
		want  string
	}{
		{PreInit, "PreInit"},
		{Init, "Init"},
		{PostInit, "PostInit"},
		{Start, "Start"},
		{Phase(99), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("Phase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestPhaseConstants(t *testing.T) {
	if PreInit != 0 {
		t.Errorf("PreInit = %d, want 0", PreInit)
	}
	if Init != 1 {
		t.Errorf("Init = %d, want 1", Init)
	}
	if PostInit != 2 {
		t.Errorf("PostInit = %d, want 2", PostInit)
	}
	if Start != 3 {
		t.Errorf("Start = %d, want 3", Start)
	}
}

func TestPluginInterface(t *testing.T) {
	p := &mockPlugin{
		name:  "test",
		phase: PreInit,
		deps:  []string{"dep1", "dep2"},
	}

	if p.Name() != "test" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test")
	}

	if p.Phase() != PreInit {
		t.Errorf("Phase() = %v, want %v", p.Phase(), PreInit)
	}

	deps := p.DependsOn()
	if len(deps) != 2 {
		t.Errorf("DependsOn() len = %d, want 2", len(deps))
	}
	if deps[0] != "dep1" || deps[1] != "dep2" {
		t.Errorf("DependsOn() = %v, want [dep1 dep2]", deps)
	}
}

func TestPluginLoad(t *testing.T) {
	t.Run("successful load", func(t *testing.T) {
		p := &mockPlugin{name: "test", phase: PreInit}
		ctx := context.Background()

		ctx, err := p.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v, want nil", err)
		}

		if !p.loadCalled {
			t.Error("Load() was not called")
		}

		if val := ctx.Value("test"); val != "loaded" {
			t.Errorf("context value = %v, want %q", val, "loaded")
		}
	})

	t.Run("load error", func(t *testing.T) {
		expectedErr := errors.New("load failed")
		p := &mockPlugin{
			name:    "test",
			phase:   PreInit,
			loadErr: expectedErr,
		}

		ctx := context.Background()
		_, err := p.Load(ctx)
		if err != expectedErr {
			t.Errorf("Load() error = %v, want %v", err, expectedErr)
		}
	})
}

func TestStarterInterface(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		p := &mockPlugin{name: "test", phase: PreInit}

		var s Starter = p
		err := s.Start(context.Background())
		if err != nil {
			t.Errorf("Start() error = %v, want nil", err)
		}

		if !p.startCalled {
			t.Error("Start() was not called")
		}
	})

	t.Run("start error", func(t *testing.T) {
		expectedErr := errors.New("start failed")
		p := &mockPlugin{
			name:     "test",
			phase:    PreInit,
			startErr: expectedErr,
		}

		var s Starter = p
		err := s.Start(context.Background())
		if err != expectedErr {
			t.Errorf("Start() error = %v, want %v", err, expectedErr)
		}
	})
}

func TestStopperInterface(t *testing.T) {
	t.Run("successful stop", func(t *testing.T) {
		p := &mockPlugin{name: "test", phase: PreInit}

		var s Stopper = p
		err := s.Stop(context.Background())
		if err != nil {
			t.Errorf("Stop() error = %v, want nil", err)
		}

		if !p.stopCalled {
			t.Error("Stop() was not called")
		}
	})

	t.Run("stop error", func(t *testing.T) {
		expectedErr := errors.New("stop failed")
		p := &mockPlugin{
			name:    "test",
			phase:   PreInit,
			stopErr: expectedErr,
		}

		var s Stopper = p
		err := s.Stop(context.Background())
		if err != expectedErr {
			t.Errorf("Stop() error = %v, want %v", err, expectedErr)
		}
	})
}

func TestPluginWithNoDependencies(t *testing.T) {
	p := &mockPlugin{
		name:  "independent",
		phase: PreInit,
		deps:  nil,
	}

	deps := p.DependsOn()
	if deps != nil && len(deps) != 0 {
		t.Errorf("DependsOn() = %v, want nil or empty", deps)
	}
}

func TestPluginLifecycle(t *testing.T) {
	p := &mockPlugin{
		name:  "lifecycle",
		phase: Init,
		deps:  []string{"dep1"},
	}

	ctx := context.Background()

	ctx, err := p.Load(ctx)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if !p.loadCalled {
		t.Error("Load() not called")
	}

	if s, ok := interface{}(p).(Starter); ok {
		if err := s.Start(ctx); err != nil {
			t.Fatalf("Start() failed: %v", err)
		}
		if !p.startCalled {
			t.Error("Start() not called")
		}
	}

	if s, ok := interface{}(p).(Stopper); ok {
		if err := s.Stop(ctx); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
		if !p.stopCalled {
			t.Error("Stop() not called")
		}
	}
}
