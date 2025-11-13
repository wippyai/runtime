// Package interceptor provides request and operation interception.
package interceptor

import (
	"context"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/runtime"
)

// mockChain implements Chain interface for testing
type mockChain struct {
	executeCalled bool
}

func (m *mockChain) Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error) {
	m.executeCalled = true
	ch := make(chan *runtime.Result, 1)
	ch <- &runtime.Result{}
	return ch, nil
}

func TestWithChainGetChain(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	chain := &mockChain{}

	ctx := WithChain(rootCtx, chain)

	retrieved := GetChain(ctx)
	if retrieved == nil {
		t.Fatal("GetChain returned nil")
	}
	if retrieved != chain {
		t.Error("GetChain returned different chain")
	}
}

func TestGetChainNoAppContext(t *testing.T) {
	ctx := context.Background()

	chain := GetChain(ctx)
	if chain != nil {
		t.Error("GetChain should return nil when no AppContext")
	}
}

func TestGetChainNotSet(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()

	chain := GetChain(rootCtx)
	if chain != nil {
		t.Error("GetChain should return nil when chain not set")
	}
}

func TestWithChainOnlySetOnce(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	chain1 := &mockChain{}
	chain2 := &mockChain{}

	ctx := WithChain(rootCtx, chain1)
	ctx = WithChain(ctx, chain2)

	retrieved := GetChain(ctx)
	if retrieved != chain1 {
		t.Error("WithChain should only set once, first value should be preserved")
	}
}

func TestSetOptionsGetOptions(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := NewBag()
	opts.Set("key1", "value1")
	opts.Set("key2", 42)

	if err := SetOptions(ctx, opts); err != nil {
		t.Fatalf("SetOptions failed: %v", err)
	}

	retrieved, ok := GetOptions(ctx)
	if !ok {
		t.Fatal("GetOptions returned false")
	}
	if retrieved == nil {
		t.Fatal("GetOptions returned nil")
	}

	if val := retrieved.GetString("key1", ""); val != "value1" {
		t.Errorf("expected key1=value1, got %s", val)
	}
	if val := retrieved.GetInt("key2", 0); val != 42 {
		t.Errorf("expected key2=42, got %d", val)
	}
}

func TestSetOptionsNoFrameContext(t *testing.T) {
	ctx := context.Background()
	opts := NewBag()

	err := SetOptions(ctx, opts)
	if err == nil {
		t.Error("SetOptions should error when no FrameContext")
	}
}

func TestGetOptionsNoFrameContext(t *testing.T) {
	ctx := context.Background()

	_, ok := GetOptions(ctx)
	if ok {
		t.Error("GetOptions should return false when no FrameContext")
	}
}

func TestGetOptionsNotSet(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	_, ok := GetOptions(ctx)
	if ok {
		t.Error("GetOptions should return false when options not set")
	}
}

func TestOptionsNotInherited(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	parentFrame, parentFC := ctxapi.OpenFrameContext(rootCtx)

	parentOpts := NewBag()
	parentOpts.Set("parent", "value")
	if err := SetOptions(parentFrame, parentOpts); err != nil {
		t.Fatalf("SetOptions failed on parent: %v", err)
	}

	// Seal parent to trigger inheritance check
	parentFC.Seal()

	childFrame, _ := ctxapi.OpenFrameContext(parentFrame)

	_, ok := GetOptions(childFrame)
	if ok {
		t.Error("Options should not inherit to child frames")
	}
}

func TestChainInAppContext(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	chain := &mockChain{}

	ctx := WithChain(rootCtx, chain)

	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	retrieved := GetChain(frameCtx)
	if retrieved == nil {
		t.Fatal("Chain should be accessible from frame context")
	}
	if retrieved != chain {
		t.Error("Chain should be same instance")
	}
}

func TestWithChainNoAppContext(t *testing.T) {
	ctx := context.Background()
	chain := &mockChain{}

	resultCtx := WithChain(ctx, chain)
	if resultCtx != ctx {
		t.Error("WithChain should return same context when no AppContext")
	}

	retrieved := GetChain(resultCtx)
	if retrieved != nil {
		t.Error("GetChain should return nil when chain not set due to missing AppContext")
	}
}

func TestInterceptorFunc_Handle(t *testing.T) {
	called := false
	nextCalled := false

	interceptor := InterceptorFunc(func(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
		called = true
		return next(ctx)
	})

	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		nextCalled = true
		return &runtime.Result{}, ctx
	}

	ctx := context.Background()
	result, _ := interceptor.Handle(ctx, next)

	if !called {
		t.Error("InterceptorFunc should have been called")
	}
	if !nextCalled {
		t.Error("next function should have been called")
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}
