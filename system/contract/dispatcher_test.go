package contract

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

func setupDispatcherTestContext(inst contract.Instantiator) context.Context {
	ctx := ctxapi.NewRootContext()
	if inst != nil {
		ctx = contract.WithContracts(ctx, nil, inst)
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

func TestOpenHandler(t *testing.T) {
	d := NewDispatcher(nil)
	mockInst := &mockInstantiator{
		instantiateFn: func(_ context.Context, _ registry.ID, _ attrs.Bag) (contract.Instance, error) {
			return &mockInstance{id: registry.NewID("test", "binding")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mockInst)
	cmd := contract.AcquireOpenCmd()
	cmd.BindingID = registry.NewID("test", "binding")

	done := make(chan contract.OpenResult, 1)
	err := d.open.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.OpenResult)
	}})

	require.NoError(t, err)
	select {
	case result := <-done:
		assert.Nil(t, result.Error)
		assert.NotNil(t, result.Instance)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestOpenHandler_NoInstantiator(t *testing.T) {
	d := NewDispatcher(nil)
	ctx := context.Background()
	cmd := contract.AcquireOpenCmd()
	cmd.BindingID = registry.NewID("test", "binding")

	done := make(chan contract.OpenResult, 1)
	err := d.open.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.OpenResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, contract.ErrInstantiatorNotFound)
}

func TestOpenHandler_Error(t *testing.T) {
	d := NewDispatcher(nil)
	expectedErr := errors.New("instantiate error")
	mockInst := &mockInstantiator{
		instantiateFn: func(_ context.Context, _ registry.ID, _ attrs.Bag) (contract.Instance, error) {
			return nil, expectedErr
		},
	}

	ctx := setupDispatcherTestContext(mockInst)
	cmd := contract.AcquireOpenCmd()
	cmd.BindingID = registry.NewID("test", "binding")

	done := make(chan contract.OpenResult, 1)
	err := d.open.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.OpenResult)
	}})

	require.NoError(t, err)
	select {
	case result := <-done:
		assert.ErrorIs(t, result.Error, expectedErr)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCallHandler(t *testing.T) {
	d := NewDispatcher(nil)
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(nil)
	cmd := contract.AcquireCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test_method"

	done := make(chan contract.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.CallResult)
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

func TestCallHandler_NilInstance(t *testing.T) {
	d := NewDispatcher(nil)
	ctx := context.Background()
	cmd := contract.AcquireCallCmd()
	cmd.Instance = nil
	cmd.Method = "test_method"

	done := make(chan contract.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.CallResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, contract.ErrInstanceNil)
}

func TestCallHandler_Error(t *testing.T) {
	d := NewDispatcher(nil)
	expectedErr := errors.New("call error")
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return nil, expectedErr
		},
	}

	ctx := setupDispatcherTestContext(nil)
	cmd := contract.AcquireCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test_method"

	done := make(chan contract.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.CallResult)
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
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Error: expectedErr}, nil
		},
	}

	ctx := setupDispatcherTestContext(nil)
	cmd := contract.AcquireCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test_method"

	done := make(chan contract.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.CallResult)
	}})

	require.NoError(t, err)
	select {
	case result := <-done:
		assert.ErrorIs(t, result.Error, expectedErr)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAsyncCallHandler(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 10)}
	d := NewDispatcher(mockNode)
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	testPID := pid.PID{Host: "test", UniqID: "1"}
	_ = runtime.SetFramePID(frameCtx, testPID)

	cmd := contract.AcquireAsyncCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "async_method"
	cmd.Topic = "@future:test-123"

	done := make(chan contract.AsyncCallResult, 1)
	err := d.asyncCall.Handle(frameCtx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.AsyncCallResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.Nil(t, result.Error)

	// Wait for async call to complete
	time.Sleep(50 * time.Millisecond)

	select {
	case pkg := <-mockNode.packages:
		assert.Equal(t, testPID, pkg.Target)
		require.Len(t, pkg.Messages, 1)
		assert.Equal(t, "@future:test-123", pkg.Messages[0].Topic)
		require.Len(t, pkg.Messages[0].Payloads, 2)
		assert.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[1]))
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for package")
	}
}

func TestAsyncCallHandler_NilInstance(t *testing.T) {
	d := NewDispatcher(&mockRelayNode{})
	ctx := context.Background()
	cmd := contract.AcquireAsyncCallCmd()
	cmd.Instance = nil
	cmd.Topic = "@future:test-123"

	done := make(chan contract.AsyncCallResult, 1)
	err := d.asyncCall.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.AsyncCallResult)
	}})

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, contract.ErrInstanceNil)
}

func TestAsyncCancelHandler(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 10)}
	d := NewDispatcher(mockNode)

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	testPID := pid.PID{Host: "test", UniqID: "1"}
	_ = runtime.SetFramePID(frameCtx, testPID)

	cmd := contract.AcquireAsyncCancelCmd()
	cmd.Topic = "@future:test-123"

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(frameCtx, cmd, 0, &testReceiver{cb: func(_ any, _ error) {
		done <- struct{}{}
	}})
	require.NoError(t, err)
	<-done

	select {
	case pkg := <-mockNode.packages:
		assert.Equal(t, testPID, pkg.Target)
		require.Len(t, pkg.Messages, 1)
		assert.Equal(t, "@future:test-123", pkg.Messages[0].Topic)
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

	assert.True(t, registered[uint16(contract.CmdOpen)])
	assert.True(t, registered[uint16(contract.CmdCall)])
	assert.True(t, registered[uint16(contract.CmdAsyncCall)])
	assert.True(t, registered[uint16(contract.CmdAsyncCancel)])
}

func TestCallHandler_Stress(t *testing.T) {
	d := NewDispatcher(nil)
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(nil)

	const numCalls = 1000
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			cmd := contract.AcquireCallCmd()
			cmd.Instance = mockInstance
			cmd.Method = "test"
			done := make(chan contract.CallResult, 1)
			err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
				done <- data.(contract.CallResult)
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

func BenchmarkCallHandler(b *testing.B) {
	d := NewDispatcher(nil)
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(nil)
	cmd := contract.AcquireCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"
	recv := &testReceiver{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.call.Handle(ctx, cmd, 0, recv)
	}
}

func BenchmarkCallHandler_Parallel(b *testing.B) {
	d := NewDispatcher(nil)
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(nil)
	cmd := contract.AcquireCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"
	recv := &testReceiver{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = d.call.Handle(ctx, cmd, 0, recv)
		}
	})
}

// Mocks

type mockInstantiator struct {
	instantiateFn func(context.Context, registry.ID, attrs.Bag) (contract.Instance, error)
}

func (m *mockInstantiator) Instantiate(ctx context.Context, bindingID registry.ID, scope attrs.Bag) (contract.Instance, error) {
	if m.instantiateFn != nil {
		return m.instantiateFn(ctx, bindingID, scope)
	}
	return nil, contract.ErrInstantiatorNotFound
}

type mockInstance struct {
	id        registry.ID
	contracts []contract.Contract
	callFn    func(context.Context, string, payload.Payloads) (*runtime.Result, error)
}

func (m *mockInstance) ID() registry.ID { return m.id }

func (m *mockInstance) Implements() []contract.Contract { return m.contracts }

func (m *mockInstance) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if m.callFn != nil {
		return m.callFn(ctx, method, input)
	}
	return nil, contract.ErrInstanceNil
}

type mockRelayNode struct {
	packages chan *relay.Package
}

func (m *mockRelayNode) ID() pid.NodeID { return "test" }

func (m *mockRelayNode) Send(pkg *relay.Package) error {
	if m.packages != nil {
		select {
		case m.packages <- pkg:
		default:
		}
	}
	return nil
}

func (m *mockRelayNode) RegisterHost(pid.HostID, relay.Receiver) error { return nil }
func (m *mockRelayNode) UnregisterHost(pid.HostID)                     {}
func (m *mockRelayNode) GetHost(pid.HostID) (relay.Receiver, bool)     { return nil, false }
func (m *mockRelayNode) Attach(pid.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockRelayNode) Detach(pid.PID) {}

func TestDispatcher_StartStop(t *testing.T) {
	d := NewDispatcher(nil)

	err := d.Start(context.Background())
	assert.NoError(t, err)

	err = d.Stop(context.Background())
	assert.NoError(t, err)
}

func TestOpenHandler_ContextCanceled(t *testing.T) {
	d := NewDispatcher(nil)
	mockInst := &mockInstantiator{
		instantiateFn: func(ctx context.Context, _ registry.ID, _ attrs.Bag) (contract.Instance, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(setupDispatcherTestContext(mockInst))

	cmd := contract.AcquireOpenCmd()
	cmd.BindingID = registry.NewID("test", "binding")

	done := make(chan contract.OpenResult, 1)
	err := d.open.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.OpenResult)
	}})
	require.NoError(t, err)

	cancel()

	select {
	case result := <-done:
		assert.Error(t, result.Error)
		assert.ErrorIs(t, result.Error, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCallHandler_ContextCanceled(t *testing.T) {
	d := NewDispatcher(nil)
	mockInstance := &mockInstance{
		callFn: func(ctx context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(setupDispatcherTestContext(nil))

	cmd := contract.AcquireCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"

	done := make(chan contract.CallResult, 1)
	err := d.call.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.CallResult)
	}})
	require.NoError(t, err)

	cancel()

	select {
	case result := <-done:
		assert.Error(t, result.Error)
		assert.ErrorIs(t, result.Error, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAsyncCallHandler_NoPID(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 1)}
	d := NewDispatcher(mockNode)

	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	cmd := contract.AcquireAsyncCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"
	cmd.Topic = "@future:test-123"

	done := make(chan contract.AsyncCallResult, 1)
	err := d.asyncCall.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.AsyncCallResult)
	}})
	require.NoError(t, err)

	select {
	case result := <-done:
		assert.ErrorIs(t, result.Error, contract.ErrPIDNotFound)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAsyncCancelHandler_NoPID(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 1)}
	d := NewDispatcher(mockNode)

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	cmd := contract.AcquireAsyncCancelCmd()
	cmd.Topic = "@future:test-123"

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(ctx, cmd, 0, &testReceiver{cb: func(_ any, _ error) {
		done <- struct{}{}
	}})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func setupAsyncTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = runtime.SetFramePID(ctx, pid.PID{Node: "test", Host: "host", UniqID: "1"})
	return ctx
}

func TestAsyncCallHandler_NoNode(t *testing.T) {
	d := NewDispatcher(nil)

	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupAsyncTestContext()

	cmd := contract.AcquireAsyncCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"
	cmd.Topic = "@future:test-123"

	done := make(chan contract.AsyncCallResult, 1)
	err := d.asyncCall.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.AsyncCallResult)
	}})
	require.NoError(t, err)

	select {
	case result := <-done:
		assert.ErrorIs(t, result.Error, contract.ErrNodeNotFound)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAsyncCancelHandler_NoNode(t *testing.T) {
	d := NewDispatcher(nil)

	ctx := setupAsyncTestContext()

	cmd := contract.AcquireAsyncCancelCmd()
	cmd.Topic = "@future:test-123"

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(ctx, cmd, 0, &testReceiver{cb: func(_ any, _ error) {
		done <- struct{}{}
	}})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAsyncCallHandler_CallError(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 1)}
	d := NewDispatcher(mockNode)

	callErr := errors.New("call failed")
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return nil, callErr
		},
	}

	ctx := setupAsyncTestContext()

	cmd := contract.AcquireAsyncCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"
	cmd.Topic = "@future:test-123"

	done := make(chan contract.AsyncCallResult, 1)
	err := d.asyncCall.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.AsyncCallResult)
	}})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}

	select {
	case pkg := <-mockNode.packages:
		assert.NotNil(t, pkg)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for package")
	}
}

func TestAsyncCallHandler_ResultError(t *testing.T) {
	mockNode := &mockRelayNode{packages: make(chan *relay.Package, 1)}
	d := NewDispatcher(mockNode)

	resultErr := errors.New("result error")
	mockInstance := &mockInstance{
		callFn: func(_ context.Context, _ string, _ payload.Payloads) (*runtime.Result, error) {
			return &runtime.Result{Error: resultErr}, nil
		},
	}

	ctx := setupAsyncTestContext()

	cmd := contract.AcquireAsyncCallCmd()
	cmd.Instance = mockInstance
	cmd.Method = "test"
	cmd.Topic = "@future:test-123"

	done := make(chan contract.AsyncCallResult, 1)
	err := d.asyncCall.Handle(ctx, cmd, 0, &testReceiver{cb: func(data any, _ error) {
		done <- data.(contract.AsyncCallResult)
	}})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}

	select {
	case pkg := <-mockNode.packages:
		assert.NotNil(t, pkg)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for package")
	}
}
