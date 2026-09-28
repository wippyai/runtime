// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	entrycfg "github.com/wippyai/runtime/system/entry"
	systempayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
)

func functionTestDecodeContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	return envapi.WithRegistry(ctx, testEnvRegistry{})
}

func decodeFunctionEntry(t *testing.T, data string, meta attrs.Bag) (*FunctionConfig, error) {
	t.Helper()
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: FunctionWASM,
		Data: payload.NewPayload(data, payload.JSON),
		Meta: meta,
	}
	return entrycfg.DecodeEntryConfigFromContext[FunctionConfig](functionTestDecodeContext(), entry)
}

func decodeWATTestEntry(t *testing.T, data string, meta attrs.Bag) (*WATFunctionConfig, error) {
	t.Helper()
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: FunctionWAT,
		Data: payload.NewPayload(data, payload.JSON),
		Meta: meta,
	}
	return entrycfg.DecodeEntryConfigFromContext[WATFunctionConfig](functionTestDecodeContext(), entry)
}

func TestFunctionConfig_FlatRootCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":100}}`,
		`{"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run","pool":{"type":"inline"}}`,
	} {
		var cfg FunctionConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
		require.NoError(t, cfg.Validate())
		if strings.Contains(raw, "limits") {
			require.Len(t, cfg.DeprecatedOptionPaths(), 1)
		} else {
			require.Empty(t, cfg.DeprecatedOptionPaths())
		}
		_, err := decodeFunctionEntry(t, raw, nil)
		require.NoError(t, err)
	}
}

func TestWATFunctionConfig_FlatRootCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"source":"(module)","method":"run","limits":{"max_execution_ms":100}}`,
		`{"source":"(module)","method":"run","pool":{"type":"inline"}}`,
	} {
		var cfg WATFunctionConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
		require.NoError(t, cfg.Validate())
		if strings.Contains(raw, "limits") {
			require.Len(t, cfg.DeprecatedOptionPaths(), 1)
		} else {
			require.Empty(t, cfg.DeprecatedOptionPaths())
		}
		_, err := decodeWATTestEntry(t, raw, nil)
		require.NoError(t, err)
	}
}

func TestFunctionConfig_UnknownFieldsInsideOptions(t *testing.T) {
	tests := []struct {
		name       string
		rawJSON    string
		errSegment string
	}{
		{
			name:       "unknown field in options",
			rawJSON:    `{"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run","options":{"unknown_field":123}}`,
			errSegment: `unknown field "unknown_field" in options`,
		},
		{
			name:       "unknown field in pool",
			rawJSON:    `{"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run","options":{"pool":{"unknown_pool_knob":123}}}`,
			errSegment: `unknown field "unknown_pool_knob" in meta.options.pool`,
		},
		{
			name:       "unknown field in limits",
			rawJSON:    `{"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run","options":{"limits":{"unknown_limit_knob":123}}}`,
			errSegment: `unknown field "unknown_limit_knob" in meta.options.limits`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeFunctionEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errSegment)
			assert.ErrorIs(t, err, ErrFunctionUnknownField)
		})
	}
}

func TestFunctionConfig_InvalidOptionTypes(t *testing.T) {
	tests := []struct {
		targetErr error
		name      string
		rawJSON   string
	}{
		{
			name: "options not object",
			rawJSON: `{
				"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":"string_not_object"}
			}`,
			targetErr: ErrFunctionOptionsInvalidType,
		},
		{
			name: "pool not object",
			rawJSON: `{
				"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"pool":"invalid"}}
			}`,
			targetErr: ErrFunctionPoolInvalidType,
		},
		{
			name: "limits not object",
			rawJSON: `{
				"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":"invalid"}}
			}`,
			targetErr: ErrFunctionLimitsInvalidType,
		},
		{
			name: "pool type not string",
			rawJSON: `{
				"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"pool":{"type":123}}}
			}`,
			targetErr: ErrFunctionPoolTypeInvalidType,
		},
		{
			name: "pool worker_class not string",
			rawJSON: `{
				"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"pool":{"worker_class":123}}}
			}`,
			targetErr: ErrFunctionPoolWorkerClassInvalidType,
		},
		{
			name: "pool warm_start not bool",
			rawJSON: `{
				"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"pool":{"warm_start":"yes"}}}
			}`,
			targetErr: ErrFunctionPoolWarmStartInvalidType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeFunctionEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.targetErr)
		})
	}
}

func TestFunctionConfig_OptionsBindingAndExplicitZero(t *testing.T) {
	raw := `{
		"fs":"app:fs","path":"/test.wasm","hash":"sha256:0","method":"run",
		"meta":{
			"options":{
				"pool":{
					"type":"static",
					"size":4,
					"workers":2,
					"buffer":16,
					"worker_class":"wasm"
				},
				"limits":{
					"max_execution_ms":5000,
					"max_open_sockets":32,
					"socket_timeout_ms":15000,
					"max_retained_memory_bytes":0,
					"retained_memory_check_interval":10
				}
			}
		}
	}`

	cfg, err := decodeFunctionEntry(t, raw, nil)
	require.NoError(t, err)

	assert.Equal(t, "static", cfg.Pool.Type)
	assert.Equal(t, 4, cfg.Pool.Size)
	assert.Equal(t, 2, cfg.Pool.Workers)
	assert.Equal(t, 16, cfg.Pool.Buffer)
	assert.Equal(t, "wasm", cfg.Pool.WorkerClass)

	assert.Equal(t, 5000, cfg.Limits.MaxExecutionMS)
	assert.Equal(t, 32, cfg.Limits.MaxOpenSockets)
	assert.Equal(t, 15000, cfg.Limits.SocketTimeoutMS)
	assert.True(t, cfg.Limits.HasMaxRetainedMemoryBytes())
	assert.Zero(t, cfg.Limits.EffectiveMaxRetainedMemoryBytes())
	assert.Equal(t, 10, cfg.Limits.RetainedMemoryCheckInterval)

	assert.Equal(t, cfg.Pool, cfg.PoolConfig())
	assert.Equal(t, cfg.Limits, cfg.LimitsConfig())
}

func TestFunctionConfig_SetOptions(t *testing.T) {
	cfg := &FunctionConfig{
		FS:     "app:fs",
		Path:   "/test.wasm",
		Hash:   "sha256:0",
		Method: "run",
	}

	opts := FunctionOptions{
		Pool: PoolConfig{
			Type: PoolTypeInline,
		},
		Limits: LimitsConfig{
			MaxExecutionMS: 2000,
		},
	}
	cfg.SetOptions(opts)
	require.NoError(t, cfg.Validate())

	assert.Equal(t, PoolTypeInline, cfg.Pool.Type)
	assert.Equal(t, 2000, cfg.Limits.MaxExecutionMS)
	assert.Equal(t, PoolTypeInline, cfg.Options().Pool.Type)
	assert.Equal(t, 2000, cfg.Options().Limits.MaxExecutionMS)
}

func TestFunctionConfig_MarshalCanonicalOptionsAndRootPool(t *testing.T) {
	cfg := FunctionConfig{
		FS:     "app:fs",
		Path:   "/test.wasm",
		Hash:   "sha256:0",
		Method: "run",
		Pool:   PoolConfig{Type: PoolTypeInline},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"pool"`)
	assert.NotContains(t, string(data), `"limits"`)

	yamlData, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(yamlData), "pool:")
	assert.NotContains(t, string(yamlData), "limits:")
}
