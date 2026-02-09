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
	assert.Equal(t, "process.wasm", ProcessWASM)
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
