// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	entrycfg "github.com/wippyai/runtime/system/entry"
	systempayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
)

func testEntryDecodeContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	return envapi.WithRegistry(ctx, testEnvRegistry{})
}

type testEnvRegistry struct{}

func (testEnvRegistry) Get(context.Context, string) (string, error) {
	return "", envapi.ErrVariableNotFound
}
func (testEnvRegistry) Lookup(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (testEnvRegistry) Set(context.Context, string, string) error      { return nil }
func (testEnvRegistry) All(context.Context) (map[string]string, error) { return nil, nil }
func (testEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}
func (testEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}
func (testEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (testEnvRegistry) UnregisterVariable(registry.ID)              {}

func validProcessJSON() string {
	return `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:abcd","method":"run"}`
}

func decodeProcessEntry(t *testing.T, data string, meta attrs.Bag) (*ProcessConfig, error) {
	t.Helper()
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "actor"),
		Kind: ProcessWASM,
		Data: payload.NewPayload(data, payload.JSON),
		Meta: meta,
	}
	return entrycfg.DecodeEntryConfigFromContext[ProcessConfig](testEntryDecodeContext(), entry)
}

func TestProcessConfig_Defaults_JSON(t *testing.T) {
	raw := validProcessJSON()

	var cfg ProcessConfig
	err := json.Unmarshal([]byte(raw), &cfg)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	assert.Equal(t, TransportTypePayload, cfg.EffectiveTransport())
	assert.Equal(t, DefaultProcessMemoryBytes, cfg.Limits().MemoryBytes)
	assert.Equal(t, DefaultProcessMemoryBytes, cfg.Limits().EffectiveMemoryBytes())
	assert.Equal(t, 0, cfg.Limits().MaxExecutionMS)
	assert.Equal(t, 0, cfg.Limits().EffectiveMaxExecutionMS())
	assert.Equal(t, DefaultMaxOpenSockets, cfg.Limits().MaxOpenSockets)
	assert.Equal(t, DefaultMaxOpenSockets, cfg.Limits().EffectiveMaxOpenSockets())
	assert.Equal(t, DefaultSocketTimeoutMS, cfg.Limits().SocketTimeoutMS)
	assert.Equal(t, DefaultSocketTimeoutMS, cfg.Limits().EffectiveSocketTimeoutMS())

	assert.Equal(t, DefaultProcessMailboxCapacity, cfg.Mailbox().Capacity)
	assert.Equal(t, DefaultProcessMailboxCapacity, cfg.Mailbox().EffectiveCapacity())
	assert.Equal(t, DefaultProcessMailboxBytes, cfg.Mailbox().Bytes)
	assert.Equal(t, DefaultProcessMailboxBytes, cfg.Mailbox().EffectiveBytes())
	assert.Equal(t, DefaultProcessMailboxMsgBytes, cfg.Mailbox().MessageBytes)
	assert.Equal(t, DefaultProcessMailboxMsgBytes, cfg.Mailbox().EffectiveMessageBytes())

	assert.Equal(t, DefaultProcessWorkerClass, cfg.WorkerClass())
	assert.Equal(t, DefaultProcessWorkerClass, cfg.Options().EffectiveWorkerClass())

	// EffectiveLimitsConfig maps to runtime LimitsConfig
	rtLimits := cfg.EffectiveLimitsConfig()
	assert.Equal(t, 0, rtLimits.MaxExecutionMS)
	assert.Equal(t, DefaultMaxOpenSockets, rtLimits.MaxOpenSockets)
	assert.Equal(t, DefaultSocketTimeoutMS, rtLimits.SocketTimeoutMS)
}

func TestProcessConfig_Defaults_EntryDecoder(t *testing.T) {
	cfg, err := decodeProcessEntry(t, validProcessJSON(), nil)
	require.NoError(t, err)

	assert.Equal(t, DefaultProcessMemoryBytes, cfg.Limits().MemoryBytes)
	assert.Equal(t, DefaultProcessMailboxCapacity, cfg.Mailbox().Capacity)
	assert.Equal(t, DefaultProcessMailboxBytes, cfg.Mailbox().Bytes)
	assert.Equal(t, DefaultProcessMailboxMsgBytes, cfg.Mailbox().MessageBytes)
	assert.Equal(t, DefaultProcessWorkerClass, cfg.WorkerClass())
}

func TestProcessConfig_Defaults_EmptyOptions(t *testing.T) {
	raw := `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:abcd","method":"run","meta":{"options":{}}}`
	cfg, err := decodeProcessEntry(t, raw, nil)
	require.NoError(t, err)

	assert.Equal(t, DefaultProcessMemoryBytes, cfg.Limits().MemoryBytes)
	assert.Equal(t, DefaultProcessMailboxCapacity, cfg.Mailbox().Capacity)
	assert.Equal(t, DefaultProcessWorkerClass, cfg.WorkerClass())
}

func TestProcessConfig_ValidNestedMetadata(t *testing.T) {
	raw := `{
		"fs": "app.fs:code",
		"path": "/actor.wasm",
		"hash": "sha256:abcd",
		"method": "main",
		"transport": "wasi-http",
		"meta": {
			"description": "actor worker",
			"options": {
				"limits": {
					"memory_bytes": 134217728,
					"max_execution_ms": 60000,
					"max_open_sockets": 64,
					"socket_timeout_ms": 10000
				},
				"mailbox": {
					"capacity": 512,
					"bytes": 16777216,
					"message_bytes": 2097152
				},
				"worker_class": "dedicated-wasm"
			}
		}
	}`

	cfg, err := decodeProcessEntry(t, raw, nil)
	require.NoError(t, err)

	// Custom description outside options is preserved in Meta
	assert.Equal(t, "actor worker", cfg.Meta.GetString("description", ""))

	assert.Equal(t, TransportTypeWASIHTTP, cfg.EffectiveTransport())
	assert.Equal(t, int64(134217728), cfg.Limits().MemoryBytes)
	assert.Equal(t, 60000, cfg.Limits().MaxExecutionMS)
	assert.Equal(t, 64, cfg.Limits().MaxOpenSockets)
	assert.Equal(t, 10000, cfg.Limits().SocketTimeoutMS)

	assert.Equal(t, 512, cfg.Mailbox().Capacity)
	assert.Equal(t, int64(16777216), cfg.Mailbox().Bytes)
	assert.Equal(t, int64(2097152), cfg.Mailbox().MessageBytes)

	assert.Equal(t, "dedicated-wasm", cfg.WorkerClass())

	rtLimits := cfg.EffectiveLimitsConfig()
	assert.Equal(t, 60000, rtLimits.MaxExecutionMS)
	assert.Equal(t, 64, rtLimits.MaxOpenSockets)
	assert.Equal(t, 10000, rtLimits.SocketTimeoutMS)
}

func TestProcessConfig_ExternalMetaOnEntry(t *testing.T) {
	meta := attrs.Bag{
		"options": map[string]any{
			"limits": map[string]any{
				"memory_bytes": int64(134217728),
			},
			"mailbox": map[string]any{
				"capacity": 256,
			},
			"worker_class": "pool-a",
		},
	}

	cfg, err := decodeProcessEntry(t, validProcessJSON(), meta)
	require.NoError(t, err)

	assert.Equal(t, int64(134217728), cfg.Limits().MemoryBytes)
	assert.Equal(t, 256, cfg.Mailbox().Capacity)
	assert.Equal(t, "pool-a", cfg.WorkerClass())
	// Defaults still apply for unconfigured fields
	assert.Equal(t, DefaultProcessMailboxBytes, cfg.Mailbox().Bytes)
	assert.Equal(t, DefaultProcessMailboxMsgBytes, cfg.Mailbox().MessageBytes)
}

func TestProcessConfig_SetOptionsProgrammatic(t *testing.T) {
	cfg := &ProcessConfig{
		FS:     "app.fs:code",
		Path:   "/actor.wasm",
		Hash:   "sha256:abcd",
		Method: "run",
	}

	cfg.SetOptions(ProcessOptions{
		Limits: ProcessLimitsConfig{
			MemoryBytes: 256 * 1024 * 1024,
		},
		Mailbox: ProcessMailboxConfig{
			Capacity:     1000,
			Bytes:        32 * 1024 * 1024,
			MessageBytes: 4 * 1024 * 1024,
		},
		WorkerClass: "isolated",
	})

	require.NoError(t, cfg.Validate())
	assert.Equal(t, int64(256*1024*1024), cfg.Limits().MemoryBytes)
	assert.Equal(t, 1000, cfg.Mailbox().Capacity)
	assert.Equal(t, int64(32*1024*1024), cfg.Mailbox().Bytes)
	assert.Equal(t, int64(4*1024*1024), cfg.Mailbox().MessageBytes)
	assert.Equal(t, "isolated", cfg.WorkerClass())
}

func TestProcessConfig_RootLimitsAndPoolRejection(t *testing.T) {
	tests := []struct {
		name      string
		rawJSON   string
		targetErr error
	}{
		{
			name:      "root limits with values",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":100}}`,
			targetErr: ErrProcessRootLimitsForbidden,
		},
		{
			name:      "root limits empty object",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","limits":{}}`,
			targetErr: ErrProcessRootLimitsForbidden,
		},
		{
			name:      "root limits explicit null",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","limits":null}`,
			targetErr: ErrProcessRootLimitsForbidden,
		},
		{
			name:      "root pool with static type",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","pool":{"type":"static","size":4}}`,
			targetErr: ErrProcessRootPoolForbidden,
		},
		{
			name:      "root pool empty object",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","pool":{}}`,
			targetErr: ErrProcessRootPoolForbidden,
		},
		{
			name:      "root pool explicit null",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","pool":null}`,
			targetErr: ErrProcessRootPoolForbidden,
		},
		{
			name:      "both root limits and meta.options",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":100},"meta":{"options":{"limits":{"memory_bytes":67108864}}}}`,
			targetErr: ErrProcessRootLimitsForbidden,
		},
		{
			name:      "both root pool and meta.options",
			rawJSON:   `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","pool":{"type":"static"},"meta":{"options":{"worker_class":"wasm"}}}`,
			targetErr: ErrProcessRootPoolForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Direct JSON unmarshal
			var cfg ProcessConfig
			err := json.Unmarshal([]byte(tt.rawJSON), &cfg)
			require.NoError(t, err)
			err = cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.targetErr)

			// Entry decode
			_, err = decodeProcessEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.targetErr)
		})
	}
}

func TestProcessConfig_UnknownFieldsInsideControls(t *testing.T) {
	tests := []struct {
		name       string
		rawJSON    string
		errSegment string
	}{
		{
			name: "unknown field in options",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"extra_knob":123}}
			}`,
			errSegment: `unknown field "extra_knob" in meta.options`,
		},
		{
			name: "unknown pool in options",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"pool":{"type":"static"}}}
			}`,
			errSegment: `unknown field "pool" in meta.options`,
		},
		{
			name: "function pooling limit inside limits",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"max_retained_memory_bytes":67108864}}}
			}`,
			errSegment: `unknown field "max_retained_memory_bytes" in meta.options.limits`,
		},
		{
			name: "check interval inside limits",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"retained_memory_check_interval":16}}}
			}`,
			errSegment: `unknown field "retained_memory_check_interval" in meta.options.limits`,
		},
		{
			name: "unknown field inside limits",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"cpu_shares":1024}}}
			}`,
			errSegment: `unknown field "cpu_shares" in meta.options.limits`,
		},
		{
			name: "unknown field inside mailbox",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"overflow_policy":"drop"}}}
			}`,
			errSegment: `unknown field "overflow_policy" in meta.options.mailbox`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeProcessEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errSegment)
			assert.ErrorIs(t, err, ErrProcessUnknownField)
		})
	}
}

func TestProcessConfig_InvalidBudgets(t *testing.T) {
	tests := []struct {
		name      string
		rawJSON   string
		targetErr error
	}{
		{
			name: "memory_bytes is 0",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"memory_bytes":0}}}
			}`,
			targetErr: ErrProcessMemoryBytesInvalid,
		},
		{
			name: "memory_bytes is negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"memory_bytes":-65536}}}
			}`,
			targetErr: ErrProcessMemoryBytesInvalid,
		},
		{
			name: "memory_bytes exceeds 4GiB",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"memory_bytes":4295032832}}}
			}`,
			targetErr: ErrProcessMemoryBytesExceeded,
		},
		{
			name: "memory_bytes not multiple of 64KiB",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"memory_bytes":65535}}}
			}`,
			targetErr: ErrProcessMemoryBytesAlignment,
		},
		{
			name: "mailbox capacity 0",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"capacity":0}}}
			}`,
			targetErr: ErrProcessMailboxCapacityInvalid,
		},
		{
			name: "mailbox capacity negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"capacity":-10}}}
			}`,
			targetErr: ErrProcessMailboxCapacityInvalid,
		},
		{
			name: "mailbox bytes 0",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"bytes":0}}}
			}`,
			targetErr: ErrProcessMailboxBytesInvalid,
		},
		{
			name: "mailbox bytes negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"bytes":-1}}}
			}`,
			targetErr: ErrProcessMailboxBytesInvalid,
		},
		{
			name: "mailbox message_bytes 0",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"message_bytes":0}}}
			}`,
			targetErr: ErrProcessMailboxMessageBytesInvalid,
		},
		{
			name: "mailbox message_bytes negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"message_bytes":-1024}}}
			}`,
			targetErr: ErrProcessMailboxMessageBytesInvalid,
		},
		{
			name: "max_execution_ms negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"max_execution_ms":-1}}}
			}`,
			targetErr: ErrInvalidExecutionLimit,
		},
		{
			name: "max_open_sockets negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"max_open_sockets":-1}}}
			}`,
			targetErr: ErrInvalidMaxOpenSockets,
		},
		{
			name: "socket_timeout_ms negative",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"socket_timeout_ms":-500}}}
			}`,
			targetErr: ErrInvalidSocketTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeProcessEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.targetErr)
		})
	}
}

func TestProcessConfig_InconsistentBudgets(t *testing.T) {
	tests := []struct {
		name    string
		rawJSON string
	}{
		{
			name: "message_bytes exceeds total mailbox bytes",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{
					"options":{
						"mailbox":{
							"bytes": 1048576,
							"message_bytes": 2097152
						}
					}
				}
			}`,
		},
		{
			name: "capacity exceeds bytes per 256 budget",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{
					"options":{
						"mailbox":{
							"bytes": 256,
							"capacity": 2
						}
					}
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeProcessEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrProcessMailboxBudgetInconsistent)
			assert.Contains(t, err.Error(), "mailbox.message_bytes cannot exceed mailbox.bytes and mailbox.capacity cannot exceed mailbox.bytes/256")
		})
	}
}

func TestProcessConfig_InvalidTypes(t *testing.T) {
	tests := []struct {
		name       string
		rawJSON    string
		errSegment string
		targetErr  error
	}{
		{
			name: "options is string",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":"invalid"}
			}`,
			targetErr: ErrProcessOptionsInvalidType,
		},
		{
			name: "options is array",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":["bad"]}
			}`,
			targetErr: ErrProcessOptionsInvalidType,
		},
		{
			name: "worker_class is number",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"worker_class":123}}
			}`,
			targetErr: ErrProcessWorkerClassInvalidType,
		},
		{
			name: "limits is string",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":"high"}}
			}`,
			targetErr: ErrProcessLimitsInvalidType,
		},
		{
			name: "mailbox is bool",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":true}}
			}`,
			targetErr: ErrProcessMailboxInvalidType,
		},
		{
			name: "memory_bytes is string",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"memory_bytes":"64MiB"}}}
			}`,
			errSegment: "must be an integer",
		},
		{
			name: "memory_bytes is fractional float",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"limits":{"memory_bytes":67108864.5}}}
			}`,
			errSegment: "must be an integer",
		},
		{
			name: "capacity is string",
			rawJSON: `{
				"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run",
				"meta":{"options":{"mailbox":{"capacity":"128"}}}
			}`,
			errSegment: "must be an integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeProcessEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
			if tt.targetErr != nil {
				assert.ErrorIs(t, err, tt.targetErr)
			}
			if tt.errSegment != "" {
				assert.ErrorContains(t, err, tt.errSegment)
			}
		})
	}
}

func TestProcessConfig_StaticFieldsValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    ProcessConfig
		targetErr error
	}{
		{
			name: "missing fs",
			config: ProcessConfig{
				Path:   "/actor.wasm",
				Hash:   "sha256:0",
				Method: "run",
			},
			targetErr: ErrFSRequired,
		},
		{
			name: "missing path",
			config: ProcessConfig{
				FS:     "app.fs:code",
				Hash:   "sha256:0",
				Method: "run",
			},
			targetErr: ErrPathRequired,
		},
		{
			name: "missing hash",
			config: ProcessConfig{
				FS:     "app.fs:code",
				Path:   "/actor.wasm",
				Method: "run",
			},
			targetErr: ErrHashRequired,
		},
		{
			name: "missing method",
			config: ProcessConfig{
				FS:   "app.fs:code",
				Path: "/actor.wasm",
				Hash: "sha256:0",
			},
			targetErr: ErrMethodRequired,
		},
		{
			name: "invalid transport",
			config: ProcessConfig{
				FS:        "app.fs:code",
				Path:      "/actor.wasm",
				Hash:      "sha256:0",
				Method:    "run",
				Transport: "invalid-transport",
			},
			targetErr: ErrInvalidTransportType,
		},
		{
			name: "empty import name",
			config: ProcessConfig{
				FS:      "app.fs:code",
				Path:    "/actor.wasm",
				Hash:    "sha256:0",
				Method:  "run",
				Imports: []registry.ID{{NS: "pkg", Name: ""}},
			},
			targetErr: ErrEmptyImportName,
		},
		{
			name: "invalid wasi non-absolute cwd",
			config: ProcessConfig{
				FS:     "app.fs:code",
				Path:   "/actor.wasm",
				Hash:   "sha256:0",
				Method: "run",
				WASI:   WASIConfig{Cwd: "relative/path"},
			},
			targetErr: ErrWASICwdMustBeAbsolute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.targetErr)
		})
	}
}

func TestProcessConfig_YAMLSerializationRoundTrip(t *testing.T) {
	rawYAML := `fs: app.fs:code
path: /actor.wasm
hash: sha256:abcd
method: run
transport: wasi-http
meta:
  options:
    limits:
      memory_bytes: 134217728
    mailbox:
      capacity: 256
    worker_class: wasm
`
	var cfg ProcessConfig
	err := yaml.Unmarshal([]byte(rawYAML), &cfg)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	assert.Equal(t, int64(134217728), cfg.Limits().MemoryBytes)
	assert.Equal(t, 256, cfg.Mailbox().Capacity)
	assert.Equal(t, "wasm", cfg.WorkerClass())

	// Re-marshal to JSON and ensure root limits/pool are not emitted
	marshaled, err := json.Marshal(cfg)
	require.NoError(t, err)

	var checkMap map[string]any
	err = json.Unmarshal(marshaled, &checkMap)
	require.NoError(t, err)
	_, hasLimits := checkMap["limits"]
	_, hasPool := checkMap["pool"]
	assert.False(t, hasLimits)
	assert.False(t, hasPool)
}

func TestProcessConfig_ErrorsKind(t *testing.T) {
	// Verify that custom errors implement apierror.Error and have Kind Invalid
	errs := []error{
		ErrProcessRootLimitsForbidden,
		ErrProcessRootPoolForbidden,
		ErrProcessOptionsInvalidType,
		ErrProcessLimitsInvalidType,
		ErrProcessMailboxInvalidType,
		ErrProcessWorkerClassInvalidType,
		ErrProcessMemoryBytesInvalid,
		ErrProcessMemoryBytesExceeded,
		ErrProcessMemoryBytesAlignment,
		ErrProcessMailboxCapacityInvalid,
		ErrProcessMailboxBytesInvalid,
		ErrProcessMailboxMessageBytesInvalid,
		ErrProcessMailboxBudgetInconsistent,
		ErrProcessUnknownField,
	}

	for _, err := range errs {
		apiErr, ok := err.(apierror.Error)
		require.True(t, ok, "error %v should implement apierror.Error", err)
		assert.Equal(t, apierror.Invalid, apiErr.Kind())
		assert.Equal(t, apierror.False, apiErr.Retryable())
	}
}

func TestProcessConfig_Security(t *testing.T) {
	rawJSON := `{
		"fs":"app.fs:code",
		"path":"/actor.wasm",
		"hash":"sha256:abcd",
		"method":"run",
		"security":{
			"actor":{"id":"app.test:actor"}
		}
	}`

	var cfg ProcessConfig
	err := json.Unmarshal([]byte(rawJSON), &cfg)
	require.NoError(t, err)
	expectedSec := &security.Config{
		Actor: security.Actor{ID: "app.test:actor"},
	}
	assert.Equal(t, expectedSec, cfg.Security)

	// JSON Roundtrip
	marshaledJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	var cfgJSON ProcessConfig
	err = json.Unmarshal(marshaledJSON, &cfgJSON)
	require.NoError(t, err)
	assert.Equal(t, cfg.Security, cfgJSON.Security)

	// YAML Roundtrip
	rawYAML := `fs: app.fs:code
path: /actor.wasm
hash: sha256:abcd
method: run
security:
  actor:
    id: app.test:actor
`
	var cfgYAML ProcessConfig
	err = yaml.Unmarshal([]byte(rawYAML), &cfgYAML)
	require.NoError(t, err)
	require.NotNil(t, cfgYAML.Security)
	assert.Equal(t, "app.test:actor", cfgYAML.Security.Actor.ID)

	marshaledYAML, err := yaml.Marshal(cfgYAML)
	require.NoError(t, err)
	var cfgYAML2 ProcessConfig
	err = yaml.Unmarshal(marshaledYAML, &cfgYAML2)
	require.NoError(t, err)
	require.NotNil(t, cfgYAML2.Security)
	assert.Equal(t, cfgYAML.Security.Actor.ID, cfgYAML2.Security.Actor.ID)
}

func TestProcessConfig_MalformedSecurity(t *testing.T) {
	tests := []struct {
		name    string
		rawJSON string
	}{
		{
			name:    "security is string",
			rawJSON: `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","security":"invalid"}`,
		},
		{
			name:    "security is number",
			rawJSON: `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","security":12345}`,
		},
		{
			name:    "security is boolean",
			rawJSON: `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","security":true}`,
		},
		{
			name:    "security actor is invalid type",
			rawJSON: `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","security":{"actor":123}}`,
		},
		{
			name:    "security groups is invalid type",
			rawJSON: `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","security":{"groups":"not-a-list"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg ProcessConfig
			err := json.Unmarshal([]byte(tt.rawJSON), &cfg)
			require.Error(t, err)

			_, err = decodeProcessEntry(t, tt.rawJSON, nil)
			require.Error(t, err)
		})
	}

	// Malformed security in YAML
	malformedYAML := `fs: app.fs:code
path: /actor.wasm
hash: sha256:abcd
method: run
security: "invalid-string"
`
	var cfgYAML ProcessConfig
	err := yaml.Unmarshal([]byte(malformedYAML), &cfgYAML)
	require.Error(t, err)
}

func TestProcessConfig_ReusedStructStateReset(t *testing.T) {
	t.Run("JSON reuse clears old options and root flags", func(t *testing.T) {
		cfg := ProcessConfig{
			FS:     "app.fs:code",
			Path:   "/actor.wasm",
			Hash:   "sha256:0",
			Method: "run",
		}

		// Step 1: Set options and validate to populate cfg.options
		cfg.SetOptions(ProcessOptions{
			WorkerClass: "custom-worker",
			Limits: ProcessLimitsConfig{
				MemoryBytes: 128 * 1024 * 1024,
			},
		})
		require.NoError(t, cfg.Validate())
		assert.Equal(t, "custom-worker", cfg.WorkerClass())
		assert.Equal(t, int64(128*1024*1024), cfg.Limits().MemoryBytes)

		// Unmarshal clean JSON without options into the SAME struct; old options must not survive
		cleanJSON := validProcessJSON()
		err := json.Unmarshal([]byte(cleanJSON), &cfg)
		require.NoError(t, err)
		assert.Equal(t, DefaultProcessWorkerClass, cfg.WorkerClass())
		assert.Equal(t, DefaultProcessMemoryBytes, cfg.Limits().MemoryBytes)
		require.NoError(t, cfg.Validate())

		// Step 2: Unmarshal root limits into cfg, verify rejection
		limitsJSON := `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":100}}`
		err = json.Unmarshal([]byte(limitsJSON), &cfg)
		require.NoError(t, err)
		require.ErrorIs(t, cfg.Validate(), ErrProcessRootLimitsForbidden)

		// Unmarshal clean JSON into the SAME struct; old rootLimits flag must not survive
		err = json.Unmarshal([]byte(cleanJSON), &cfg)
		require.NoError(t, err)
		require.NoError(t, cfg.Validate())

		// Step 3: Unmarshal root pool into cfg, verify rejection
		poolJSON := `{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:0","method":"run","pool":{"type":"static"}}`
		err = json.Unmarshal([]byte(poolJSON), &cfg)
		require.NoError(t, err)
		require.ErrorIs(t, cfg.Validate(), ErrProcessRootPoolForbidden)

		// Unmarshal clean JSON into the SAME struct; old rootPool flag must not survive
		err = json.Unmarshal([]byte(cleanJSON), &cfg)
		require.NoError(t, err)
		require.NoError(t, cfg.Validate())
	})

	t.Run("YAML reuse clears old options and root flags", func(t *testing.T) {
		cfg := ProcessConfig{
			FS:     "app.fs:code",
			Path:   "/actor.wasm",
			Hash:   "sha256:0",
			Method: "run",
		}

		// Step 1: Set options and validate
		cfg.SetOptions(ProcessOptions{
			WorkerClass: "custom-worker",
			Limits: ProcessLimitsConfig{
				MemoryBytes: 128 * 1024 * 1024,
			},
		})
		require.NoError(t, cfg.Validate())
		assert.Equal(t, "custom-worker", cfg.WorkerClass())
		assert.Equal(t, int64(128*1024*1024), cfg.Limits().MemoryBytes)

		// Unmarshal clean YAML without options into the SAME struct; old options must not survive
		cleanYAML := `fs: app.fs:code
path: /actor.wasm
hash: sha256:0
method: run
`
		err := yaml.Unmarshal([]byte(cleanYAML), &cfg)
		require.NoError(t, err)
		assert.Equal(t, DefaultProcessWorkerClass, cfg.WorkerClass())
		assert.Equal(t, DefaultProcessMemoryBytes, cfg.Limits().MemoryBytes)
		require.NoError(t, cfg.Validate())

		// Step 2: Unmarshal root limits into cfg via YAML
		limitsYAML := `fs: app.fs:code
path: /actor.wasm
hash: sha256:0
method: run
limits:
  max_execution_ms: 100
`
		err = yaml.Unmarshal([]byte(limitsYAML), &cfg)
		require.NoError(t, err)
		require.ErrorIs(t, cfg.Validate(), ErrProcessRootLimitsForbidden)

		// Unmarshal clean YAML into SAME struct; old rootLimits flag must not survive
		err = yaml.Unmarshal([]byte(cleanYAML), &cfg)
		require.NoError(t, err)
		require.NoError(t, cfg.Validate())

		// Step 3: Unmarshal root pool into cfg via YAML
		poolYAML := `fs: app.fs:code
path: /actor.wasm
hash: sha256:0
method: run
pool:
  type: static
`
		err = yaml.Unmarshal([]byte(poolYAML), &cfg)
		require.NoError(t, err)
		require.ErrorIs(t, cfg.Validate(), ErrProcessRootPoolForbidden)

		// Unmarshal clean YAML into SAME struct; old rootPool flag must not survive
		err = yaml.Unmarshal([]byte(cleanYAML), &cfg)
		require.NoError(t, err)
		require.NoError(t, cfg.Validate())
	})
}

func TestProcessDurationOverflowRejected(t *testing.T) {
	for _, key := range []string{"max_execution_ms", "socket_timeout_ms"} {
		t.Run(key, func(t *testing.T) {
			var cfg ProcessConfig
			require.NoError(t, json.Unmarshal([]byte(validProcessJSON()), &cfg))
			cfg.Meta = attrs.Bag{"options": map[string]any{"limits": map[string]any{key: int64(9223372036855)}}}
			require.Error(t, cfg.Validate())
		})
	}
}
