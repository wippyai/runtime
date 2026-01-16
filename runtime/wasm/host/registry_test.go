package host

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

const (
	testCmd1 dispatcher.CommandID = 1
	testCmd2 dispatcher.CommandID = 2
	testCmd3 dispatcher.CommandID = 3
)

type mockHost struct {
	namespace   string
	description string
	class       []string
	functions   map[string]any
	yieldTypes  []wasmapi.YieldType
}

func (h *mockHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   h.namespace,
		Description: h.description,
		Class:       h.class,
	}
}

func (h *mockHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions:  h.functions,
		YieldTypes: h.yieldTypes,
	}
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()

	assert.NotNil(t, r)
	assert.NotNil(t, r.hosts)
	assert.NotNil(t, r.yieldTypes)
	assert.Empty(t, r.hosts)
	assert.Empty(t, r.yieldTypes)
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	host := &mockHost{
		namespace:   "test:host",
		description: "Test host",
		class:       []string{wasmapi.ClassDeterministic},
		functions:   map[string]any{"foo": func() {}},
		yieldTypes:  []wasmapi.YieldType{{CmdID: testCmd1}},
	}

	err := r.Register(host)

	require.NoError(t, err)
	assert.Len(t, r.hosts, 1)
	assert.Len(t, r.yieldTypes, 1)
}

func TestRegistry_Register_EmptyNamespace(t *testing.T) {
	r := NewRegistry()

	host := &mockHost{
		namespace: "",
	}

	err := r.Register(host)

	require.Error(t, err)
	assert.Equal(t, ErrEmptyNamespace, err)
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()

	host1 := &mockHost{namespace: "test:host"}
	host2 := &mockHost{namespace: "test:host"}

	err := r.Register(host1)
	require.NoError(t, err)

	err = r.Register(host2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	host := &mockHost{namespace: "test:host"}
	_ = r.Register(host)

	got, ok := r.Get("test:host")
	assert.True(t, ok)
	assert.Equal(t, host, got)

	_, ok = r.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()

	host1 := &mockHost{namespace: "test:host1"}
	host2 := &mockHost{namespace: "test:host2"}
	_ = r.Register(host1)
	_ = r.Register(host2)

	all := r.All()

	assert.Len(t, all, 2)
}

func TestRegistry_Namespaces(t *testing.T) {
	r := NewRegistry()

	_ = r.Register(&mockHost{namespace: "test:a"})
	_ = r.Register(&mockHost{namespace: "test:b"})
	_ = r.Register(&mockHost{namespace: "test:c"})

	ns := r.Namespaces()

	assert.Len(t, ns, 3)
	assert.Contains(t, ns, "test:a")
	assert.Contains(t, ns, "test:b")
	assert.Contains(t, ns, "test:c")
}

func TestRegistry_YieldTypes(t *testing.T) {
	r := NewRegistry()

	host := &mockHost{
		namespace: "test:host",
		yieldTypes: []wasmapi.YieldType{
			{CmdID: testCmd1},
			{CmdID: testCmd2},
		},
	}
	_ = r.Register(host)

	types := r.YieldTypes()

	assert.Len(t, types, 2)
}

func TestRegistry_HasYieldType(t *testing.T) {
	r := NewRegistry()

	host := &mockHost{
		namespace: "test:host",
		yieldTypes: []wasmapi.YieldType{
			{CmdID: testCmd1},
		},
	}
	_ = r.Register(host)

	assert.True(t, r.HasYieldType(testCmd1))
	assert.False(t, r.HasYieldType(testCmd2))
}

func TestRegistry_MultipleHosts_YieldTypes(t *testing.T) {
	r := NewRegistry()

	host1 := &mockHost{
		namespace:  "test:host1",
		yieldTypes: []wasmapi.YieldType{{CmdID: testCmd1}, {CmdID: testCmd2}},
	}
	host2 := &mockHost{
		namespace:  "test:host2",
		yieldTypes: []wasmapi.YieldType{{CmdID: testCmd3}},
	}

	_ = r.Register(host1)
	_ = r.Register(host2)

	types := r.YieldTypes()
	assert.Len(t, types, 3)

	assert.True(t, r.HasYieldType(testCmd1))
	assert.True(t, r.HasYieldType(testCmd2))
	assert.True(t, r.HasYieldType(testCmd3))
}

func TestRegistry_EmptyFunctions(t *testing.T) {
	r := NewRegistry()

	host := &mockHost{
		namespace: "test:empty",
		functions: nil,
	}

	err := r.Register(host)
	require.NoError(t, err)

	got, ok := r.Get("test:empty")
	assert.True(t, ok)
	assert.NotNil(t, got)
}
