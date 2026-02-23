// SPDX-License-Identifier: MPL-2.0

package temporal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
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

type mockRunHandoff struct {
	runs map[string]string
}

func (m *mockRunHandoff) Publish(clientID, workflowID, runID string) {
	if m.runs == nil {
		m.runs = make(map[string]string)
	}
	m.runs[clientID+":"+workflowID] = runID
}

func (m *mockRunHandoff) Consume(clientID, workflowID string) (string, bool) {
	if m.runs == nil {
		return "", false
	}
	key := clientID + ":" + workflowID
	v, ok := m.runs[key]
	if ok {
		delete(m.runs, key)
	}
	return v, ok
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

func TestWorkflowRunHandoffRegistry(t *testing.T) {
	t.Run("set and get registry", func(t *testing.T) {
		reg := &mockRunHandoff{}
		ctx := WithWorkflowRunHandoff(context.Background(), reg)

		got := GetWorkflowRunHandoff(ctx)
		require.NotNil(t, got)
		assert.Equal(t, reg, got)
	})

	t.Run("get from empty context returns nil", func(t *testing.T) {
		got := GetWorkflowRunHandoff(context.Background())
		assert.Nil(t, got)
	})

	t.Run("publish and consume", func(t *testing.T) {
		reg := &mockRunHandoff{}
		ctx := WithWorkflowRunHandoff(context.Background(), reg)

		got := GetWorkflowRunHandoff(ctx)
		got.Publish("client-1", "wf-1", "run-1")

		runID, ok := got.Consume("client-1", "wf-1")
		require.True(t, ok)
		assert.Equal(t, "run-1", runID)

		_, ok = got.Consume("client-1", "wf-1")
		assert.False(t, ok)
	})
}

func TestWorkerIDContext(t *testing.T) {
	t.Run("set and get worker id", func(t *testing.T) {
		ctx := WithWorkerID(context.Background(), "app.test.temporal:test_worker")
		assert.Equal(t, "app.test.temporal:test_worker", GetWorkerID(ctx))
	})

	t.Run("missing worker id returns empty", func(t *testing.T) {
		assert.Equal(t, "", GetWorkerID(context.Background()))
	})

	t.Run("sealed frame falls back to context value", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, frame := ctxapi.OpenFrameContext(ctx)
		require.NotNil(t, frame)
		frame.Seal()
		defer ctxapi.ReleaseFrameContext(frame)

		ctx = WithWorkerID(ctx, "app.test.temporal:test_worker")
		assert.Equal(t, "app.test.temporal:test_worker", GetWorkerID(ctx))
	})
}

func TestClientIDContext(t *testing.T) {
	t.Run("set and get client id", func(t *testing.T) {
		ctx := WithClientID(context.Background(), "app.test.temporal:test_client")
		assert.Equal(t, "app.test.temporal:test_client", GetClientID(ctx))
	})

	t.Run("missing client id returns empty", func(t *testing.T) {
		assert.Equal(t, "", GetClientID(context.Background()))
	})

	t.Run("sealed frame falls back to context value", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, frame := ctxapi.OpenFrameContext(ctx)
		require.NotNil(t, frame)
		frame.Seal()
		defer ctxapi.ReleaseFrameContext(frame)

		ctx = WithClientID(ctx, "app.test.temporal:test_client")
		assert.Equal(t, "app.test.temporal:test_client", GetClientID(ctx))
	})
}
