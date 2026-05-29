// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

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
