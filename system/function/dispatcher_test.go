package function

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

func setupDispatcherTestContext(reg function.Registry) context.Context {
	ctx := ctxapi.NewRootContext()
	if reg != nil {
		ctx = function.WithRegistry(ctx, reg)
	}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

// testReceiver implements ResultReceiver for tests
type testReceiver struct {
	cb func(data any, err error)
}

func (r *testReceiver) CompleteYield(_ uint64, data any, err error) {
	if r.cb != nil {
		r.cb(data, err)
	}
}

func TestCallHandler(t *testing.T) {
	d := NewDispatcher(nil)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := function.AcquireCallCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}

	done := make(chan function.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(function.CallResult)
	}})

	require.NoError(t, err)
	select {
	case result := <-done:
		assert.Nil(t, result.Error)
		assert.NotNil(t, result.Value)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCallHandler_NoRegistry(t *testing.T) {
	d := NewDispatcher(nil)
	ctx := context.Background()
	cmd := function.AcquireCallCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}

	done := make(chan function.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(function.CallResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, function.ErrRegistryNotFound)
}

func TestCallHandler_Error(t *testing.T) {
	d := NewDispatcher(nil)
	expectedErr := errors.New("call error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return nil, expectedErr
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := function.AcquireCallCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}

	done := make(chan function.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(function.CallResult)
	}})

	require.NoError(t, err)
	select {
	case result := <-done:
		assert.ErrorIs(t, result.Error, expectedErr)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCallHandler_ResultError(t *testing.T) {
	d := NewDispatcher(nil)
	expectedErr := errors.New("result error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Error: expectedErr}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := function.AcquireCallCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}

	done := make(chan function.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(function.CallResult)
	}})

	require.NoError(t, err)
	select {
	case result := <-done:
		assert.ErrorIs(t, result.Error, expectedErr)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAsyncStartHandler(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 10)}
	d := NewDispatcher(mockNode)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := ctxapi.NewRootContext()
	ctx = function.WithRegistry(ctx, mock)
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	testPID := relay.PID{Host: "test", UniqID: "1"}
	_ = runtime.SetFramePID(frameCtx, testPID)

	cmd := function.AcquireAsyncStartCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
	cmd.Topic = "@future:test-123"

	done := make(chan function.AsyncStartResult, 1)
	err := d.asyncStart.Handle(frameCtx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(function.AsyncStartResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.Nil(t, result.Error)

	// Wait for the async call to complete and send package
	time.Sleep(50 * time.Millisecond)

	select {
	case pkg := <-mockNode.packages:
		assert.Equal(t, testPID, pkg.Target)
		require.Len(t, pkg.Messages, 1)
		assert.Equal(t, "@future:test-123", string(pkg.Messages[0].Topic))
		// Should have result payload + terminal
		require.Len(t, pkg.Messages[0].Payloads, 2)
		assert.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[1]))
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for package")
	}
}

func TestAsyncStartHandler_NoRegistry(t *testing.T) {
	d := NewDispatcher(&mockRelayNode{})
	ctx := context.Background()
	cmd := function.AcquireAsyncStartCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
	cmd.Topic = "@future:test-123"

	done := make(chan function.AsyncStartResult, 1)
	err := d.asyncStart.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(function.AsyncStartResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, function.ErrRegistryNotFound)
}

func TestAsyncCancelHandler(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 10)}
	d := NewDispatcher(mockNode)

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	testPID := relay.PID{Host: "test", UniqID: "1"}
	_ = runtime.SetFramePID(frameCtx, testPID)

	cmd := function.AcquireAsyncCancelCmd()
	cmd.Topic = "@future:test-123"

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(frameCtx, cmd, 0, &testReceiver{cb: func(_ any, _ error) {
		done <- struct{}{}
	}})
	require.NoError(t, err)
	<-done

	// Should have sent terminal package
	select {
	case pkg := <-mockNode.packages:
		assert.Equal(t, testPID, pkg.Target)
		require.Len(t, pkg.Messages, 1)
		assert.Equal(t, "@future:test-123", string(pkg.Messages[0].Topic))
		require.Len(t, pkg.Messages[0].Payloads, 1)
		assert.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[0]))
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for package")
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher(nil)

	registered := make(map[uint16]bool)
	register := func(id dispatcher.CommandID, _ dispatcher.Handler) {
		registered[uint16(id)] = true
	}

	d.RegisterAll(register)

	assert.True(t, registered[uint16(function.Call)])
	assert.True(t, registered[uint16(function.AsyncStart)])
	assert.True(t, registered[uint16(function.AsyncCancel)])
}

func BenchmarkCallHandler(b *testing.B) {
	d := NewDispatcher(nil)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := function.AcquireCallCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
	recv := &testReceiver{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.call.Handle(ctx, cmd, 0, recv)
	}
}

func BenchmarkCallHandler_Parallel(b *testing.B) {
	d := NewDispatcher(nil)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := function.AcquireCallCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
	recv := &testReceiver{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = d.call.Handle(ctx, cmd, 0, recv)
		}
	})
}

// Stress tests

func TestCallHandler_Stress(t *testing.T) {
	d := NewDispatcher(nil)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)

	const numCalls = 1000
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			cmd := function.AcquireCallCmd()
			cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
			done := make(chan function.CallResult, 1)
			err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
				done <- data.(function.CallResult)
			}})
			assert.NoError(t, err)
			select {
			case result := <-done:
				assert.Nil(t, result.Error)
				assert.NotNil(t, result.Value)
			case <-time.After(time.Second):
				t.Error("timeout waiting for result")
			}
		}()
	}

	wg.Wait()
}

func BenchmarkAsyncStartHandler(b *testing.B) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 100000)}
	d := NewDispatcher(mockNode)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := ctxapi.NewRootContext()
	ctx = function.WithRegistry(ctx, mock)
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	testPID := relay.PID{Host: "test", UniqID: "1"}
	_ = runtime.SetFramePID(frameCtx, testPID)

	cmd := function.AcquireAsyncStartCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
	cmd.Topic = "@future:test"
	recv := &testReceiver{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.asyncStart.Handle(frameCtx, cmd, 0, recv)
	}
}

func BenchmarkAsyncStartHandler_Parallel(b *testing.B) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 100000)}
	d := NewDispatcher(mockNode)
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := ctxapi.NewRootContext()
	ctx = function.WithRegistry(ctx, mock)
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	testPID := relay.PID{Host: "test", UniqID: "1"}
	_ = runtime.SetFramePID(frameCtx, testPID)

	cmd := function.AcquireAsyncStartCmd()
	cmd.Task = runtime.Task{ID: registry.NewID("test", "func")}
	cmd.Topic = "@future:test"
	recv := &testReceiver{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = d.asyncStart.Handle(frameCtx, cmd, 0, recv)
		}
	})
}

// mockRelayNode implements relay.Node for testing
type mockRelayNode struct {
	packages chan *relay.Package
}

func (m *mockRelayNode) ID() relay.NodeID { return "test" }

func (m *mockRelayNode) Send(pkg *relay.Package) error {
	if m.packages != nil {
		select {
		case m.packages <- pkg:
		default:
		}
	}
	return nil
}

func (m *mockRelayNode) RegisterHost(relay.HostID, relay.Host) error { return nil }
func (m *mockRelayNode) UnregisterHost(relay.HostID)                 {}
func (m *mockRelayNode) GetHost(relay.HostID) (relay.Host, bool)     { return nil, false }
func (m *mockRelayNode) Attach(relay.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockRelayNode) Detach(relay.PID) {}
