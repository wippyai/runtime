// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	syspayload "github.com/wippyai/runtime/system/payload"
	payloadjson "github.com/wippyai/runtime/system/payload/json"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func layeredOverrideConfig(layers ...map[string]any) boot.Config {
	inputs := make([]boot.Config, 0, len(layers))
	resolved := make(map[string]any)
	for _, layer := range layers {
		inputs = append(inputs, boot.NewConfig(boot.WithSection("override", layer)))
		for key, value := range layer {
			resolved[key] = value
		}
	}
	return boot.WithConfigLayers(boot.NewConfig(boot.WithSection("override", resolved)), inputs...)
}

func executeOverride(t *testing.T, cfg boot.Config, entries *[]registry.Entry) error {
	t.Helper()
	ctx, _ := setupTestContext()
	return Override().Execute(boot.WithConfig(ctx, cfg), entries)
}

func TestOverride_WASMOptionAliasLayersUseExistingLegacyGroup(t *testing.T) {
	source := map[string]any{"limits": map[string]any{"max_execution_ms": 1}}
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "function.wasm",
		Data: payload.New(source),
	}}

	err := executeOverride(t, layeredOverrideConfig(
		map[string]any{"app:converter:limits.max_execution_ms": 10},
		map[string]any{"app:converter:options.limits.max_execution_ms": 20},
	), &entries)
	if err != nil {
		t.Fatal(err)
	}
	data := entries[0].Data.Data().(map[string]any)
	limits := data["limits"].(map[string]any)
	if limits["max_execution_ms"] != 20 {
		t.Fatalf("legacy limits value = %v, want 20", limits["max_execution_ms"])
	}
	if _, exists := data["options"]; exists {
		t.Fatal("canonical override created a second options group")
	}
	if source["limits"].(map[string]any)["max_execution_ms"] != 1 {
		t.Fatal("override mutated source payload")
	}
}

func TestOverride_WASMOptionAliasesConflictWithinLayerBeforeWrites(t *testing.T) {
	source := map[string]any{"limits": map[string]any{"max_execution_ms": 1}}
	configSource := map[string]any{
		"app:converter:limits.max_execution_ms":     10,
		"app:converter:options.limits.memory_bytes": int64(20),
	}
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "function.wasm",
		Data: payload.New(source),
	}}

	err := executeOverride(t, layeredOverrideConfig(configSource), &entries)
	if err == nil || !containsString(err.Error(), "duplicate WASM option override") {
		t.Fatalf("expected alias conflict, got %v", err)
	}
	if !reflect.DeepEqual(entries[0].Data.Data(), source) || source["limits"].(map[string]any)["max_execution_ms"] != 1 {
		t.Fatal("collection error mutated entry or source")
	}
	if configSource["app:converter:limits.max_execution_ms"] != 10 {
		t.Fatal("collection mutated config input")
	}
}

func TestOverride_WASMSiblingLeavesSameSpellingCoexist(t *testing.T) {
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "function.wasm",
		Data: payload.New(map[string]any{"limits": map[string]any{}}),
	}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{
		"app:converter:limits.max_execution_ms": 10,
		"app:converter:limits.memory_bytes":     int64(20),
	}), &entries)
	if err != nil {
		t.Fatal(err)
	}
	limits := entries[0].Data.Data().(map[string]any)["limits"].(map[string]any)
	if limits["max_execution_ms"] != 10 || limits["memory_bytes"] != int64(20) {
		t.Fatalf("limits = %#v", limits)
	}
}

func TestOverride_WASMMetaAliasAndDataMetaRemainDistinct(t *testing.T) {
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "function.wasm",
		Data: payload.New(map[string]any{"meta": map[string]any{"options": map[string]any{"limits": map[string]any{"max_execution_ms": 1}}}}),
		Meta: map[string]any{"options": map[string]any{"limits": map[string]any{"max_execution_ms": 2}}},
	}}
	err := executeOverride(t, layeredOverrideConfig(
		map[string]any{"app:converter:data.meta.options.limits.max_execution_ms": 10},
		map[string]any{"app:converter:meta.options.limits.max_execution_ms": 20},
	), &entries)
	if err != nil {
		t.Fatal(err)
	}
	dataMeta := entries[0].Data.Data().(map[string]any)["meta"].(map[string]any)
	if dataMeta["options"].(map[string]any)["limits"].(map[string]any)["max_execution_ms"] != 10 {
		t.Fatal("data.meta path was treated as registry metadata")
	}
	meta := entries[0].Meta["options"].(map[string]any)
	if meta["limits"].(map[string]any)["max_execution_ms"] != 20 {
		t.Fatal("meta.options alias was not applied to metadata")
	}
}

func TestOverride_ClonesNestedAttrsBagWithoutDroppingSiblings(t *testing.T) {
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "function.wasm",
		Data: payload.New(map[string]any{}),
		Meta: map[string]any{"options": attrs.Bag{"limits": attrs.Bag{"max_execution_ms": 1, "memory_bytes": int64(2)}}},
	}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{
		"app:converter:meta.options.limits.max_execution_ms": 10,
	}), &entries)
	if err != nil {
		t.Fatal(err)
	}
	limits := entries[0].Meta["options"].(map[string]any)["limits"].(map[string]any)
	if limits["max_execution_ms"] != 10 || limits["memory_bytes"] != int64(2) {
		t.Fatalf("nested metadata changed unexpectedly: %#v", limits)
	}
}

func TestOverride_KindIsClassifiedBeforeWASMOptions(t *testing.T) {
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "process.lua",
		Data: payload.New(map[string]any{}),
	}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{
		"app:converter:kind":                    "function.wasm",
		"app:converter:limits.max_execution_ms": 10,
	}), &entries)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Kind != "function.wasm" {
		t.Fatalf("kind = %q", entries[0].Kind)
	}
	if got := entries[0].Data.Data().(map[string]any)["limits"].(map[string]any)["max_execution_ms"]; got != 10 {
		t.Fatalf("limit = %v", got)
	}
}

func TestOverride_WASMWholeOptionsContainerUpdatesExistingAlias(t *testing.T) {
	entries := []registry.Entry{{
		ID:   registry.NewID("app", "converter"),
		Kind: "function.wasm",
		Data: payload.New(map[string]any{"limits": map[string]any{"max_execution_ms": 1}}),
	}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{
		"app:converter:options": map[string]any{"limits": map[string]any{"max_execution_ms": 10}},
	}), &entries)
	require.NoError(t, err)
	if got := entries[0].Data.Data().(map[string]any)["limits"].(map[string]any)["max_execution_ms"]; got != 10 {
		t.Fatalf("whole-container override failed: %v", got)
	}
}

func TestOverride_WASMJSONPayloadUsesExistingAlias(t *testing.T) {
	source := []byte(`{"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":1}}`)
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: "function.wasm", Data: payload.NewPayload(source, payload.JSON)}}
	cfg := layeredOverrideConfig(map[string]any{"app:converter:options.limits.max_execution_ms": 10})
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	transcoder := syspayload.NewTranscoder()
	payloadjson.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	require.NoError(t, Override().Execute(boot.WithConfig(ctx, cfg), &entries))
	data := entries[0].Data.Data().(map[string]any)
	require.NotContains(t, data, "options")
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	var config wasmapi.FunctionConfig
	require.NoError(t, json.Unmarshal(encoded, &config))
	require.NoError(t, config.Validate())
	require.Equal(t, 10, config.Limits.MaxExecutionMS)
	require.Contains(t, string(source), `"max_execution_ms":1`)
}

func TestOverride_WholeLegacyOptionsPreservesInvocationReplacement(t *testing.T) {
	source := map[string]any{"options": map[string]any{"limits": map[string]any{"max_execution_ms": 1}}}
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: "function.wasm", Data: payload.New(source), Meta: attrs.Bag{"options": map[string]any{"retry": 2, "old_default": true}}}}
	replacement := map[string]any{"limits": map[string]any{"max_execution_ms": 20}, "retry": 4}
	require.NoError(t, executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:meta.options": replacement}), &entries))
	data := entries[0].Data.Data().(map[string]any)
	require.Equal(t, 20, data["options"].(map[string]any)["limits"].(map[string]any)["max_execution_ms"])
	require.Equal(t, map[string]any{"retry": 4}, entries[0].Meta["options"])
	require.Contains(t, replacement, "limits", "caller input must remain intact")
	require.Equal(t, 1, source["options"].(map[string]any)["limits"].(map[string]any)["max_execution_ms"])
}

func TestOverride_WholeOptionsAndAliasConflictBeforeWrites(t *testing.T) {
	source := map[string]any{"limits": map[string]any{"max_execution_ms": 1}}
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: "function.wasm", Data: payload.New(source)}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:options": map[string]any{"limits": map[string]any{"max_execution_ms": 20}}, "app:converter:limits.max_open_sockets": 3}), &entries)
	require.Error(t, err)
	require.Equal(t, source, entries[0].Data.Data())
}

func TestOverride_BasicSet(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"addr": ":8080",
				"tls":  false,
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:gateway:addr": ":9090",
			"app:gateway:tls":  true,
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	entry := entries[0]
	data := entry.Data.Data().(map[string]any)

	if data["addr"] != ":9090" {
		t.Errorf("Expected addr=:9090, got %v", data["addr"])
	}

	if data["tls"] != true {
		t.Errorf("Expected tls=true, got %v", data["tls"])
	}
}

func TestOverride_NestedPath(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"connection": map[string]any{
					"host": "localhost",
					"port": 5432,
				},
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"db:main:connection.host": "db.example.com",
			"db:main:connection.port": 3306,
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	entry := entries[0]
	data := entry.Data.Data().(map[string]any)

	conn, ok := data["connection"].(map[string]any)
	if !ok {
		t.Fatalf("Expected connection to be map, got %T", data["connection"])
	}

	if conn["host"] != "db.example.com" {
		t.Errorf("Expected host=db.example.com, got %v", conn["host"])
	}

	if conn["port"] != 3306 {
		t.Errorf("Expected port=3306, got %v", conn["port"])
	}
}

func TestOverride_KindPathChangesEntryKind(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "db"),
			Kind: "db.sql.sqlite",
			Data: payload.New(map[string]any{
				"kind": "data-kind-stays-data",
				"file": ".wippy/app.db",
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:db:kind":      "db.sql.postgres",
			"app:db:data.kind": "explicit-data-kind",
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if entries[0].Kind != "db.sql.postgres" {
		t.Fatalf("Kind = %q, want db.sql.postgres", entries[0].Kind)
	}

	data := entries[0].Data.Data().(map[string]any)
	if data["kind"] != "explicit-data-kind" {
		t.Fatalf("data.kind = %v, want explicit-data-kind", data["kind"])
	}
}

func TestOverride_MetaPath(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"count": 2,
			}),
			Meta: map[string]any{
				"priority": "low",
			},
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:worker:meta.priority": "high",
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	entry := entries[0]

	if entry.Meta["priority"] != "high" {
		t.Errorf("Expected meta.priority=high, got %v", entry.Meta["priority"])
	}
}

func TestOverride_DataPrefix(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "cache"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"ttl": 300,
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:cache:data.ttl": 600,
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	entry := entries[0]
	data := entry.Data.Data().(map[string]any)

	if data["ttl"] != 600 {
		t.Errorf("Expected ttl=600, got %v", data["ttl"])
	}
}

func TestOverride_MultipleEntries(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"addr": ":8080",
			}),
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"count": 2,
			}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"host": "localhost",
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:gateway:addr": ":9090",
			"app:worker:count": 4,
			"db:main:host":     "db.example.com",
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	gateway := entries[0].Data.Data().(map[string]any)
	if gateway["addr"] != ":9090" {
		t.Errorf("Gateway: expected addr=:9090, got %v", gateway["addr"])
	}

	worker := entries[1].Data.Data().(map[string]any)
	if worker["count"] != 4 {
		t.Errorf("Worker: expected count=4, got %v", worker["count"])
	}

	db := entries[2].Data.Data().(map[string]any)
	if db["host"] != "db.example.com" {
		t.Errorf("DB: expected host=db.example.com, got %v", db["host"])
	}
}

func TestOverride_NoConfig(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"addr": ":8080",
			}),
		},
	}

	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data := entries[0].Data.Data().(map[string]any)

	if data["addr"] != ":8080" {
		t.Errorf("Expected addr unchanged at :8080, got %v", data["addr"])
	}
}

func TestOverride_EmptySection(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"addr": ":8080",
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("other", map[string]any{
			"app:gateway:addr": ":9090",
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data := entries[0].Data.Data().(map[string]any)

	if data["addr"] != ":8080" {
		t.Errorf("Expected addr unchanged at :8080, got %v", data["addr"])
	}
}

func TestOverride_EntryNotFound(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"addr": ":8080",
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:notfound:addr": ":9090",
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override()

	err := stage.Execute(ctx, &entries)
	if err == nil {
		t.Fatal("Expected error for entry not found, got nil")
	}

	if !containsString(err.Error(), "no entry found") {
		t.Errorf("Expected 'no entry found' error, got %v", err)
	}
}

func TestOverride_EntryNotFoundIgnoredWhenConfigured(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"addr": ":8080",
			}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:notfound:addr": ":9090",
			"app:gateway:addr":  ":8081",
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Override(WithMissingOverrideEntriesIgnored())

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data := entries[0].Data.Data().(map[string]any)
	if data["addr"] != ":8081" {
		t.Errorf("Expected addr=:8081, got %v", data["addr"])
	}
}

func TestParseOverrideKey_Valid(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		wantNamespace string
		wantEntry     string
		wantPath      string
	}{
		{
			name:          "simple path",
			key:           "app:gateway:addr",
			wantNamespace: "app",
			wantEntry:     "gateway",
			wantPath:      "addr",
		},
		{
			name:          "nested path",
			key:           "app:gateway:data.addr",
			wantNamespace: "app",
			wantEntry:     "gateway",
			wantPath:      "data.addr",
		},
		{
			name:          "meta path",
			key:           "app:worker:meta.priority",
			wantNamespace: "app",
			wantEntry:     "worker",
			wantPath:      "meta.priority",
		},
		{
			name:          "deeply nested",
			key:           "db:main:connection.pool.max",
			wantNamespace: "db",
			wantEntry:     "main",
			wantPath:      "connection.pool.max",
		},
		{
			name:          "dots in namespace",
			key:           "app.v2:gateway:addr",
			wantNamespace: "app.v2",
			wantEntry:     "gateway",
			wantPath:      "addr",
		},
		{
			name:          "dots in entry name",
			key:           "app:gateway.v1:addr",
			wantNamespace: "app",
			wantEntry:     "gateway.v1",
			wantPath:      "addr",
		},
		{
			name:          "dots in both",
			key:           "app.v2:gateway.v1:data.addr",
			wantNamespace: "app.v2",
			wantEntry:     "gateway.v1",
			wantPath:      "data.addr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, entryName, path, err := parseOverrideKey(tt.key)
			if err != nil {
				t.Fatalf("parseOverrideKey() error = %v", err)
			}

			if namespace != tt.wantNamespace {
				t.Errorf("namespace = %v, want %v", namespace, tt.wantNamespace)
			}
			if entryName != tt.wantEntry {
				t.Errorf("entryName = %v, want %v", entryName, tt.wantEntry)
			}
			if path != tt.wantPath {
				t.Errorf("path = %v, want %v", path, tt.wantPath)
			}
		})
	}
}

func TestParseOverrideKey_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{
			name:    "empty key",
			key:     "",
			wantErr: "empty key",
		},
		{
			name:    "missing first colon",
			key:     "appgatewayaddr",
			wantErr: "missing first ':'",
		},
		{
			name:    "missing second colon",
			key:     "app:gateway",
			wantErr: "empty path",
		},
		{
			name:    "empty namespace",
			key:     ":gateway:addr",
			wantErr: "empty namespace",
		},
		{
			name:    "empty entry name",
			key:     "app::addr",
			wantErr: "empty entry name",
		},
		{
			name:    "empty path",
			key:     "app:gateway:",
			wantErr: "empty path",
		},
		{
			name:    "no remainder after first colon",
			key:     "app:",
			wantErr: "missing entry name and path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseOverrideKey(tt.key)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if !containsString(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestOverride_CannotEraseAmbiguousSourceDeclaration(t *testing.T) {
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: "function.wasm", Data: payload.New(map[string]any{"limits": map[string]any{"max_execution_ms": 1}}), Meta: attrs.Bag{"options": map[string]any{"limits": map[string]any{"max_execution_ms": 2}}}}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:meta.options": map[string]any{"retry": 3}}), &entries)
	require.ErrorContains(t, err, "duplicate option group")
	require.Contains(t, entries[0].Meta["options"], "limits")
}
