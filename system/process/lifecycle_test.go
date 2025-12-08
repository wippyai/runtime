package process

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

type mockLifecycle struct {
	onStartCalled    bool
	onCompleteCalled bool
	startPID         relay.PID
	completePID      relay.PID
	completeResult   *runtime.Result
	mu               sync.Mutex
}

func (m *mockLifecycle) OnStart(_ context.Context, pid relay.PID, _ process.Process) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStartCalled = true
	m.startPID = pid
}

func (m *mockLifecycle) OnComplete(_ context.Context, pid relay.PID, result *runtime.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCompleteCalled = true
	m.completePID = pid
	m.completeResult = result
}

func TestLifecycleRegistry_Register(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc1 := &mockLifecycle{}
	lc2 := &mockLifecycle{}

	reg.Register("lc1", lc1)
	reg.Register("lc2", lc2)

	pid := relay.PID{UniqID: "test-pid"}
	reg.OnStart(context.Background(), pid, nil)

	assert.True(t, lc1.onStartCalled)
	assert.True(t, lc2.onStartCalled)
	assert.Equal(t, pid, lc1.startPID)
	assert.Equal(t, pid, lc2.startPID)
}

func TestLifecycleRegistry_Unregister(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc1 := &mockLifecycle{}
	lc2 := &mockLifecycle{}

	reg.Register("lc1", lc1)
	reg.Register("lc2", lc2)
	reg.Unregister("lc1")

	pid := relay.PID{UniqID: "test-pid"}
	reg.OnStart(context.Background(), pid, nil)

	assert.False(t, lc1.onStartCalled)
	assert.True(t, lc2.onStartCalled)
}

func TestLifecycleRegistry_OnComplete(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc := &mockLifecycle{}
	reg.Register("lc", lc)

	pid := relay.PID{UniqID: "test-pid"}
	result := &runtime.Result{Value: nil, Error: nil}

	reg.OnComplete(context.Background(), pid, result)

	assert.True(t, lc.onCompleteCalled)
	assert.Equal(t, pid, lc.completePID)
	assert.Equal(t, result, lc.completeResult)
}

func TestLifecycleRegistry_ReplaceExisting(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc1 := &mockLifecycle{}
	lc2 := &mockLifecycle{}

	reg.Register("same-name", lc1)
	reg.Register("same-name", lc2)

	pid := relay.PID{UniqID: "test-pid"}
	reg.OnStart(context.Background(), pid, nil)

	assert.False(t, lc1.onStartCalled)
	assert.True(t, lc2.onStartCalled)
}

func TestLifecycleRegistry_OrderPreserved(t *testing.T) {
	reg := NewLifecycleRegistry()

	var order []string
	var mu sync.Mutex

	makeLC := func(name string) process.Lifecycle {
		return &orderTrackingLifecycle{
			name:  name,
			order: &order,
			mu:    &mu,
		}
	}

	reg.Register("first", makeLC("first"))
	reg.Register("second", makeLC("second"))
	reg.Register("third", makeLC("third"))

	pid := relay.PID{UniqID: "test-pid"}
	reg.OnStart(context.Background(), pid, nil)

	assert.Equal(t, []string{"first", "second", "third"}, order)
}

type orderTrackingLifecycle struct {
	name  string
	order *[]string
	mu    *sync.Mutex
}

func (o *orderTrackingLifecycle) OnStart(_ context.Context, _ relay.PID, _ process.Process) {
	o.mu.Lock()
	defer o.mu.Unlock()
	*o.order = append(*o.order, o.name)
}

func (o *orderTrackingLifecycle) OnComplete(_ context.Context, _ relay.PID, _ *runtime.Result) {
}

func TestLifecycleRegistry_ConcurrentAccess(_ *testing.T) {
	reg := NewLifecycleRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lc := &mockLifecycle{}
			name := string(rune('a' + i%26))
			reg.Register(name, lc)
			reg.OnStart(context.Background(), relay.PID{UniqID: "test"}, nil)
		}(i)
	}
	wg.Wait()
}

var _ process.LifecycleRegistry = (*LifecycleRegistry)(nil)
