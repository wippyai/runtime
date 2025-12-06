package interceptor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.temporal.io/sdk/interceptor"
)

// mockClientInterceptor implements interceptor.ClientInterceptor for testing
type mockClientInterceptor struct {
	interceptor.ClientInterceptorBase
	name string
}

func (m *mockClientInterceptor) InterceptClient(next interceptor.ClientOutboundInterceptor) interceptor.ClientOutboundInterceptor {
	return next
}

func TestClientRegistry_Register(t *testing.T) {
	t.Run("register single interceptor", func(t *testing.T) {
		registry := NewClientRegistry()
		interceptor1 := &mockClientInterceptor{name: "interceptor1"}

		registry.Register(interceptor1)

		all := registry.GetAll()
		assert.Len(t, all, 1)
		assert.Equal(t, interceptor1, all[0])
	})

	t.Run("register multiple interceptors", func(t *testing.T) {
		registry := NewClientRegistry()
		interceptor1 := &mockClientInterceptor{name: "interceptor1"}
		interceptor2 := &mockClientInterceptor{name: "interceptor2"}
		interceptor3 := &mockClientInterceptor{name: "interceptor3"}

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
		registry := NewClientRegistry()
		interceptors := make([]*mockClientInterceptor, 10)
		for i := 0; i < 10; i++ {
			interceptors[i] = &mockClientInterceptor{name: string(rune('A' + i))}
			registry.Register(interceptors[i])
		}

		all := registry.GetAll()
		assert.Len(t, all, 10)
		for i, interceptor := range all {
			assert.Equal(t, interceptors[i], interceptor)
		}
	})
}

func TestClientRegistry_GetAll(t *testing.T) {
	t.Run("empty registry returns empty slice", func(t *testing.T) {
		registry := NewClientRegistry()

		all := registry.GetAll()
		assert.NotNil(t, all)
		assert.Len(t, all, 0)
	})

	t.Run("returns copy of interceptors", func(t *testing.T) {
		registry := NewClientRegistry()
		interceptor1 := &mockClientInterceptor{name: "interceptor1"}
		registry.Register(interceptor1)

		all1 := registry.GetAll()
		all2 := registry.GetAll()

		assert.Len(t, all1, 1)
		assert.Len(t, all2, 1)
		assert.NotSame(t, &all1, &all2, "should return different slices")
	})

	t.Run("modifying returned slice does not affect registry", func(t *testing.T) {
		registry := NewClientRegistry()
		interceptor1 := &mockClientInterceptor{name: "interceptor1"}
		registry.Register(interceptor1)

		all := registry.GetAll()
		_ = append(all, &mockClientInterceptor{name: "fake"})

		allAgain := registry.GetAll()
		assert.Len(t, allAgain, 1, "registry should not be affected by modifications to returned slice")
	})
}

func TestClientRegistry_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent register and get", func(t *testing.T) {
		registry := NewClientRegistry()
		done := make(chan bool)

		go func() {
			for i := 0; i < 100; i++ {
				registry.Register(&mockClientInterceptor{name: "writer"})
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
		registry := NewClientRegistry()
		numWriters := 10
		writesPerWriter := 10
		done := make(chan bool, numWriters)

		for w := 0; w < numWriters; w++ {
			go func() {
				for i := 0; i < writesPerWriter; i++ {
					registry.Register(&mockClientInterceptor{name: "writer"})
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

func TestClientRegistry_NilInterceptor(t *testing.T) {
	t.Run("register nil interceptor", func(t *testing.T) {
		registry := NewClientRegistry()

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

func TestClientRegistry_RealInterceptorInterface(t *testing.T) {
	t.Run("verify interface compatibility", func(t *testing.T) {
		registry := NewClientRegistry()

		interceptor1 := &mockClientInterceptor{name: "real"}

		registry.Register(interceptor1)
		all := registry.GetAll()
		assert.Len(t, all, 1)
	})
}
