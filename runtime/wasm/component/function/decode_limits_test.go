// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	entrycfg "github.com/wippyai/runtime/system/entry"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
)

func limitsDecodeContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	return envapi.WithRegistry(ctx, limitsDecodeEnvRegistry{})
}

type limitsDecodeEnvRegistry struct{}

func (limitsDecodeEnvRegistry) Get(context.Context, string) (string, error) {
	return "", envapi.ErrVariableNotFound
}
func (limitsDecodeEnvRegistry) Lookup(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (limitsDecodeEnvRegistry) Set(context.Context, string, string) error      { return nil }
func (limitsDecodeEnvRegistry) All(context.Context) (map[string]string, error) { return nil, nil }
func (limitsDecodeEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}
func (limitsDecodeEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}
func (limitsDecodeEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (limitsDecodeEnvRegistry) UnregisterVariable(registry.ID)              {}

func decodeWATEntry(t *testing.T, data string) *wasmapi.WATFunctionConfig {
	t.Helper()

	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: wasmapi.FunctionWAT,
		Data: payload.NewPayload(data, payload.JSON),
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[wasmapi.WATFunctionConfig](limitsDecodeContext(), entry)
	require.NoError(t, err)
	return cfg
}

func TestDecodeWATEntryLimitsOmittedResolveToDefaults(t *testing.T) {
	cfg := decodeWATEntry(t, `{"source":"(module)","method":"handle"}`)

	assert.Zero(t, cfg.Limits.MaxRetainedMemoryBytes)
	assert.Zero(t, cfg.Limits.RetainedMemoryCheckInterval)
	assert.Equal(t, wasmapi.DefaultMaxRetainedMemoryBytes, cfg.Limits.EffectiveMaxRetainedMemoryBytes())
	assert.Equal(t, wasmapi.DefaultRetainedMemoryCheckInterval, cfg.Limits.EffectiveRetainedMemoryCheckInterval())
}

func TestDecodeWATEntryLimitsBindConfiguredKeys(t *testing.T) {
	cfg := decodeWATEntry(t, `{"source":"(module)","method":"handle","limits":{"max_execution_ms":250,"max_retained_memory_bytes":1048576,"retained_memory_check_interval":4}}`)

	assert.Equal(t, 250, cfg.Limits.MaxExecutionMS)
	assert.Equal(t, int64(1048576), cfg.Limits.MaxRetainedMemoryBytes)
	assert.Equal(t, 4, cfg.Limits.RetainedMemoryCheckInterval)
	assert.Equal(t, int64(1048576), cfg.Limits.EffectiveMaxRetainedMemoryBytes())
	assert.Equal(t, 4, cfg.Limits.EffectiveRetainedMemoryCheckInterval())
}

func TestDecodeWATEntryExplicitZeroDisablesRetainedMemoryLimit(t *testing.T) {
	cfg := decodeWATEntry(t, `{"source":"(module)","method":"handle","limits":{"max_retained_memory_bytes":0}}`)

	assert.True(t, cfg.Limits.HasMaxRetainedMemoryBytes())
	assert.Zero(t, cfg.Limits.EffectiveMaxRetainedMemoryBytes())
}

func TestDecodeWASMEntryLimitsBindConfiguredKeys(t *testing.T) {
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: wasmapi.FunctionWASM,
		Data: payload.NewPayload(`{"fs":"app.fs:data","path":"/fn.wasm","hash":"sha256:0","method":"handle","limits":{"max_retained_memory_bytes":2097152,"retained_memory_check_interval":8}}`, payload.JSON),
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[wasmapi.FunctionConfig](limitsDecodeContext(), entry)
	require.NoError(t, err)

	assert.Equal(t, int64(2097152), cfg.Limits.MaxRetainedMemoryBytes)
	assert.Equal(t, 8, cfg.Limits.RetainedMemoryCheckInterval)
	assert.Equal(t, 8, cfg.Limits.EffectiveRetainedMemoryCheckInterval())
}
