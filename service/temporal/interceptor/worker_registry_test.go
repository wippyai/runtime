package interceptor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
)

// mockWorkerInterceptor implements interceptor.WorkerInterceptor for testing
type mockWorkerInterceptor struct {
	interceptor.WorkerInterceptorBase
	name string
}

func TestWorkerRegistry_Register(t *testing.T) {
	t.Run("register single interceptor", func(t *testing.T) {
		registry := NewWorkerRegistry()
		interceptor1 := &mockWorkerInterceptor{name: "interceptor1"}

		registry.Register(interceptor1)

		all := registry.GetAll()
		assert.Len(t, all, 1)
		assert.Equal(t, interceptor1, all[0])
	})

	t.Run("register multiple interceptors", func(t *testing.T) {
		registry := NewWorkerRegistry()
		interceptor1 := &mockWorkerInterceptor{name: "interceptor1"}
		interceptor2 := &mockWorkerInterceptor{name: "interceptor2"}
		interceptor3 := &mockWorkerInterceptor{name: "interceptor3"}

		registry.Register(interceptor1)
		registry.Register(interceptor2)
		registry.Register(interceptor3)

		all := registry.GetAll()
		assert.Len(t, all, 3)
		assert.Equal(t, interceptor1, all[0])
		assert.Equal(t, interceptor2, all[1])
		assert.Equal(t, interceptor3, all[2])
	})

	t.Run("register maintains order", func(t *testing.T) {
		registry := NewWorkerRegistry()
		interceptors := make([]*mockWorkerInterceptor, 10)
		for i := 0; i < 10; i++ {
			interceptors[i] = &mockWorkerInterceptor{name: string(rune('A' + i))}
			registry.Register(interceptors[i])
		}

		all := registry.GetAll()
		assert.Len(t, all, 10)
		for i, interceptor := range all {
			assert.Equal(t, interceptors[i], interceptor)
		}
	})
}

func TestWorkerRegistry_GetAll(t *testing.T) {
	t.Run("empty registry returns empty slice", func(t *testing.T) {
		registry := NewWorkerRegistry()

		all := registry.GetAll()
		assert.NotNil(t, all)
		assert.Len(t, all, 0)
	})

	t.Run("returns copy of interceptors", func(t *testing.T) {
		registry := NewWorkerRegistry()
		interceptor1 := &mockWorkerInterceptor{name: "interceptor1"}
		registry.Register(interceptor1)

		all1 := registry.GetAll()
		all2 := registry.GetAll()

		assert.Len(t, all1, 1)
		assert.Len(t, all2, 1)
		assert.NotSame(t, &all1, &all2, "should return different slices")
	})

	t.Run("modifying returned slice does not affect registry", func(t *testing.T) {
		registry := NewWorkerRegistry()
		interceptor1 := &mockWorkerInterceptor{name: "interceptor1"}
		registry.Register(interceptor1)

		all := registry.GetAll()
		_ = append(all, &mockWorkerInterceptor{name: "fake"})

		allAgain := registry.GetAll()
		assert.Len(t, allAgain, 1, "registry should not be affected by modifications to returned slice")
	})
}

func TestWorkerRegistry_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent register and get", func(t *testing.T) {
		registry := NewWorkerRegistry()
		done := make(chan bool)

		go func() {
			for i := 0; i < 100; i++ {
				registry.Register(&mockWorkerInterceptor{name: "writer"})
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 100; i++ {
				_ = registry.GetAll()
			}
			done <- true
		}()

		<-done
		<-done

		all := registry.GetAll()
		assert.Len(t, all, 100)
	})

	t.Run("multiple concurrent writers", func(t *testing.T) {
		registry := NewWorkerRegistry()
		numWriters := 10
		writesPerWriter := 10
		done := make(chan bool, numWriters)

		for w := 0; w < numWriters; w++ {
			go func() {
				for i := 0; i < writesPerWriter; i++ {
					registry.Register(&mockWorkerInterceptor{name: "writer"})
				}
				done <- true
			}()
		}

		for i := 0; i < numWriters; i++ {
			<-done
		}

		all := registry.GetAll()
		assert.Len(t, all, numWriters*writesPerWriter)
	})
}

func TestWorkerRegistry_NilInterceptor(t *testing.T) {
	t.Run("register nil interceptor", func(t *testing.T) {
		registry := NewWorkerRegistry()

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("should not panic when registering nil interceptor")
			}
		}()

		registry.Register(nil)
		all := registry.GetAll()
		assert.Len(t, all, 1)
		assert.Nil(t, all[0])
	})
}

func TestWorkerRegistry_RealWorkerOptions(t *testing.T) {
	t.Run("use in worker options", func(t *testing.T) {
		registry := NewWorkerRegistry()

		interceptor1 := &mockWorkerInterceptor{name: "interceptor1"}
		interceptor2 := &mockWorkerInterceptor{name: "interceptor2"}

		registry.Register(interceptor1)
		registry.Register(interceptor2)

		workerOptions := worker.Options{
			Interceptors: registry.GetAll(),
		}

		assert.Len(t, workerOptions.Interceptors, 2)
		assert.Equal(t, interceptor1, workerOptions.Interceptors[0])
		assert.Equal(t, interceptor2, workerOptions.Interceptors[1])
	})
}
