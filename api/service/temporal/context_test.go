package temporal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/interceptor"
)

type mockClientInterceptor struct {
	interceptor.ClientInterceptorBase
}

type mockWorkerInterceptor struct {
	interceptor.WorkerInterceptorBase
}

type mockClientRegistry struct {
	interceptors []interceptor.ClientInterceptor
}

func (r *mockClientRegistry) Register(i interceptor.ClientInterceptor) {
	r.interceptors = append(r.interceptors, i)
}

func (r *mockClientRegistry) GetAll() []interceptor.ClientInterceptor {
	return r.interceptors
}

type mockWorkerRegistry struct {
	interceptors []interceptor.WorkerInterceptor
}

func (r *mockWorkerRegistry) Register(i interceptor.WorkerInterceptor) {
	r.interceptors = append(r.interceptors, i)
}

func (r *mockWorkerRegistry) GetAll() []interceptor.WorkerInterceptor {
	return r.interceptors
}

func TestClientInterceptorRegistry(t *testing.T) {
	t.Run("set and get registry", func(t *testing.T) {
		reg := &mockClientRegistry{}
		ctx := WithClientInterceptorRegistry(context.Background(), reg)

		got := GetClientInterceptorRegistry(ctx)
		require.NotNil(t, got)
		assert.Equal(t, reg, got)
	})

	t.Run("get from empty context returns nil", func(t *testing.T) {
		got := GetClientInterceptorRegistry(context.Background())
		assert.Nil(t, got)
	})

	t.Run("register and retrieve interceptors", func(t *testing.T) {
		reg := &mockClientRegistry{}
		ctx := WithClientInterceptorRegistry(context.Background(), reg)

		interceptor1 := &mockClientInterceptor{}
		interceptor2 := &mockClientInterceptor{}

		got := GetClientInterceptorRegistry(ctx)
		got.Register(interceptor1)
		got.Register(interceptor2)

		all := got.GetAll()
		assert.Len(t, all, 2)
	})
}

func TestWorkerInterceptorRegistry(t *testing.T) {
	t.Run("set and get registry", func(t *testing.T) {
		reg := &mockWorkerRegistry{}
		ctx := WithWorkerInterceptorRegistry(context.Background(), reg)

		got := GetWorkerInterceptorRegistry(ctx)
		require.NotNil(t, got)
		assert.Equal(t, reg, got)
	})

	t.Run("get from empty context returns nil", func(t *testing.T) {
		got := GetWorkerInterceptorRegistry(context.Background())
		assert.Nil(t, got)
	})

	t.Run("register and retrieve interceptors", func(t *testing.T) {
		reg := &mockWorkerRegistry{}
		ctx := WithWorkerInterceptorRegistry(context.Background(), reg)

		interceptor1 := &mockWorkerInterceptor{}
		interceptor2 := &mockWorkerInterceptor{}

		got := GetWorkerInterceptorRegistry(ctx)
		got.Register(interceptor1)
		got.Register(interceptor2)

		all := got.GetAll()
		assert.Len(t, all, 2)
	})
}
