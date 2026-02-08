package wasm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventConstants(t *testing.T) {
	assert.Equal(t, "wasm", System)
	assert.Equal(t, "wasm.reset_code", InvalidateNodes)
}

func TestKindConstants(t *testing.T) {
	assert.Equal(t, "function.wat", FunctionWAT)
	assert.Equal(t, "function.wasm", FunctionWASM)
}

func TestPoolConstants(t *testing.T) {
	assert.Equal(t, 100, DefaultMaxSize)
	assert.Equal(t, "lazy", PoolTypeLazy)
	assert.Equal(t, "static", PoolTypeStatic)
	assert.Equal(t, "inline", PoolTypeInline)
	assert.Equal(t, "adaptive", PoolTypeAdaptive)
}

func TestTransportConstants(t *testing.T) {
	assert.Equal(t, "payload", TransportTypePayload)
	assert.Equal(t, "wasi-http", TransportTypeWASIHTTP)
}

func TestSecurityDefaultConstants(t *testing.T) {
	assert.True(t, DefaultInheritActor)
	assert.True(t, DefaultInheritScope)
	assert.True(t, DefaultInheritRequestContext)
}

func TestClassConstants(t *testing.T) {
	assert.Equal(t, "deterministic", ClassDeterministic)
	assert.Equal(t, "nondeterministic", ClassNondeterministic)
	assert.Equal(t, "io", ClassIO)
	assert.Equal(t, "network", ClassNetwork)
	assert.Equal(t, "time", ClassTime)
	assert.Equal(t, "storage", ClassStorage)
	assert.Equal(t, "process", ClassProcess)
	assert.Equal(t, "security", ClassSecurity)
}
