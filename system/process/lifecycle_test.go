// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

type mockLifecycle struct {
	completeResult   *runtime.Result
	startPID         pid.PID
	completePID      pid.PID
	mu               sync.Mutex
	onStartCalled    bool
	onCompleteCalled bool
}

func (m *mockLifecycle) OnStart(_ context.Context, p pid.PID, _ process.Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStartCalled = true
	m.startPID = p
	return nil
}

func (m *mockLifecycle) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCompleteCalled = true
	m.completePID = p
	m.completeResult = result
}

func TestLifecycleRegistry_Register(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc1 := &mockLifecycle{}
	lc2 := &mockLifecycle{}

	reg.Register("lc1", lc1)
	reg.Register("lc2", lc2)

	p := pid.PID{UniqID: "test-pid"}
	err := reg.OnStart(context.Background(), p, nil)
	assert.NoError(t, err)

	assert.True(t, lc1.onStartCalled)
	assert.True(t, lc2.onStartCalled)
	assert.Equal(t, p, lc1.startPID)
	assert.Equal(t, p, lc2.startPID)
}

func TestLifecycleRegistry_Unregister(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc1 := &mockLifecycle{}
	lc2 := &mockLifecycle{}

	reg.Register("lc1", lc1)
	reg.Register("lc2", lc2)
	reg.Unregister("lc1")

	p := pid.PID{UniqID: "test-pid"}
	err := reg.OnStart(context.Background(), p, nil)
	assert.NoError(t, err)

	assert.False(t, lc1.onStartCalled)
	assert.True(t, lc2.onStartCalled)
}

func TestLifecycleRegistry_OnComplete(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc := &mockLifecycle{}
	reg.Register("lc", lc)

	p := pid.PID{UniqID: "test-pid"}
	result := &runtime.Result{Value: nil, Error: nil}

	reg.OnComplete(context.Background(), p, result)

	assert.True(t, lc.onCompleteCalled)
	assert.Equal(t, p, lc.completePID)
	assert.Equal(t, result, lc.completeResult)
}

func TestLifecycleRegistry_ReplaceExisting(t *testing.T) {
	reg := NewLifecycleRegistry()

	lc1 := &mockLifecycle{}
	lc2 := &mockLifecycle{}

	reg.Register("same-name", lc1)
	reg.Register("same-name", lc2)

	p := pid.PID{UniqID: "test-pid"}
	err := reg.OnStart(context.Background(), p, nil)
	assert.NoError(t, err)

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

	p := pid.PID{UniqID: "test-pid"}
	err := reg.OnStart(context.Background(), p, nil)
	assert.NoError(t, err)

	assert.Equal(t, []string{"first", "second", "third"}, order)
}

type orderTrackingLifecycle struct {
	order *[]string
	mu    *sync.Mutex
	name  string
}

func (o *orderTrackingLifecycle) OnStart(_ context.Context, _ pid.PID, _ process.Process) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	*o.order = append(*o.order, o.name)
	return nil
}

func (o *orderTrackingLifecycle) OnComplete(_ context.Context, _ pid.PID, _ *runtime.Result) {
}

func TestLifecycleRegistry_OnStartError(t *testing.T) {
	reg := NewLifecycleRegistry()

	testErr := errors.New("lifecycle error")
	errorLC := &errorLifecycle{err: testErr}
	successLC := &mockLifecycle{}

	reg.Register("first", errorLC)
	reg.Register("second", successLC)

	p := pid.PID{UniqID: "test-pid"}
	err := reg.OnStart(context.Background(), p, nil)

	assert.Error(t, err)
	assert.Equal(t, testErr, err)
	// Second handler should not be called since first errored
	assert.False(t, successLC.onStartCalled)
}

type errorLifecycle struct {
	err error
}

func (e *errorLifecycle) OnStart(_ context.Context, _ pid.PID, _ process.Process) error {
	return e.err
}

func (e *errorLifecycle) OnComplete(_ context.Context, _ pid.PID, _ *runtime.Result) {
}

func TestLifecycleRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewLifecycleRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lc := &mockLifecycle{}
			name := string(rune('a' + i%26))
			reg.Register(name, lc)
			_ = reg.OnStart(context.Background(), pid.PID{UniqID: "test"}, nil)
		}(i)
	}

	// Should complete without deadlock or panic
	assert.NotPanics(t, func() {
		wg.Wait()
	})
}

var _ process.LifecycleRegistry = (*LifecycleRegistry)(nil)

func BenchmarkLifecycleRegistry_OnStart_2(b *testing.B) {
	reg := NewLifecycleRegistry()
	reg.Register("lc1", &mockLifecycle{})
	reg.Register("lc2", &mockLifecycle{})

	ctx := context.Background()
	p := pid.PID{UniqID: "bench-pid"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = reg.OnStart(ctx, p, nil)
	}
}

func BenchmarkLifecycleRegistry_OnStart_10(b *testing.B) {
	reg := NewLifecycleRegistry()
	for i := 0; i < 10; i++ {
		reg.Register(string(rune('a'+i)), &mockLifecycle{})
	}

	ctx := context.Background()
	p := pid.PID{UniqID: "bench-pid"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = reg.OnStart(ctx, p, nil)
	}
}

func BenchmarkLifecycleRegistry_OnComplete_2(b *testing.B) {
	reg := NewLifecycleRegistry()
	reg.Register("lc1", &mockLifecycle{})
	reg.Register("lc2", &mockLifecycle{})

	ctx := context.Background()
	p := pid.PID{UniqID: "bench-pid"}
	result := &runtime.Result{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.OnComplete(ctx, p, result)
	}
}

func BenchmarkLifecycleRegistry_OnComplete_10(b *testing.B) {
	reg := NewLifecycleRegistry()
	for i := 0; i < 10; i++ {
		reg.Register(string(rune('a'+i)), &mockLifecycle{})
	}

	ctx := context.Background()
	p := pid.PID{UniqID: "bench-pid"}
	result := &runtime.Result{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg.OnComplete(ctx, p, result)
	}
}
