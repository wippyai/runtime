package app

import (
	"fmt"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/runtimeconfig"
	"go.uber.org/zap/zaptest"
)

func TestApplyFieldPathToEntry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	app := &App{
		logger: logger,
	}

	tests := []struct {
		name         string
		entry        regapi.Entry
		fieldPath    string
		value        interface{}
		expectedAddr string
		expectedErr  bool
	}{
		{
			name: "update addr field in data",
			entry: regapi.Entry{
				ID:   regapi.ParseID("system:gateway"),
				Kind: "http.service",
				Data: payload.New(map[string]interface{}{
					"addr": ":8086",
					"kind": "http.service",
				}),
			},
			fieldPath:    "addr",
			value:        "8090",
			expectedAddr: "8090",
			expectedErr:  false,
		},
		{
			name: "update nested field in data",
			entry: regapi.Entry{
				ID:   regapi.ParseID("system:gateway"),
				Kind: "http.service",
				Data: payload.New(map[string]interface{}{
					"timeouts": map[string]interface{}{
						"read": "30s",
					},
				}),
			},
			fieldPath:    "timeouts.read",
			value:        "60s",
			expectedAddr: "", // not checking addr here
			expectedErr:  false,
		},
		{
			name: "update field with data prefix",
			entry: regapi.Entry{
				ID:   regapi.ParseID("system:gateway"),
				Kind: "http.service",
				Data: payload.New(map[string]interface{}{
					"addr": ":8086",
				}),
			},
			fieldPath:    "data.addr",
			value:        "8090",
			expectedAddr: "8090",
			expectedErr:  false,
		},
		{
			name: "update meta field",
			entry: regapi.Entry{
				ID:   regapi.ParseID("system:gateway"),
				Kind: "http.service",
				Meta: regapi.Metadata{
					"comment": "Original comment",
				},
				Data: payload.New(map[string]interface{}{
					"addr": ":8086",
				}),
			},
			fieldPath:    "meta.comment",
			value:        "Updated comment",
			expectedAddr: ":8086", // addr should remain unchanged
			expectedErr:  false,
		},
		{
			name: "create new field in empty data",
			entry: regapi.Entry{
				ID:   regapi.ParseID("system:gateway"),
				Kind: "http.service",
				Data: payload.New(map[string]interface{}{}),
			},
			fieldPath:    "addr",
			value:        "8090",
			expectedAddr: "8090",
			expectedErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the entry to avoid modifying the test case
			entryCopy := tt.entry

			// Apply the field path update
			valueStr := fmt.Sprintf("%v", tt.value)
			err := app.applyFieldPathToEntry(&entryCopy, tt.fieldPath, valueStr)

			if tt.expectedErr {
				if err == nil {
					t.Errorf("applyFieldPathToEntry() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("applyFieldPathToEntry() unexpected error: %v", err)
				return
			}

			// Verify the data was updated correctly
			data := entryCopy.Data.Data()
			if data == nil {
				t.Errorf("applyFieldPathToEntry() data is nil after update")
				return
			}

			dataMap, ok := data.(map[string]interface{})
			if !ok {
				t.Errorf("applyFieldPathToEntry() data is not a map after update, got %T", data)
				return
			}

			// Check addr if expected
			if tt.expectedAddr != "" {
				if addr, exists := dataMap["addr"]; exists {
					if addrStr, ok := addr.(string); ok {
						if addrStr != tt.expectedAddr {
							t.Errorf("applyFieldPathToEntry() addr = %q, want %q", addrStr, tt.expectedAddr)
						}
					} else {
						t.Errorf("applyFieldPathToEntry() addr is not a string, got %T", addr)
					}
				} else {
					t.Errorf("applyFieldPathToEntry() addr field not found in data")
				}
			}

			// Verify entry pointer was modified (not just a copy)
			if entryCopy.ID != tt.entry.ID {
				t.Errorf("applyFieldPathToEntry() entry ID changed unexpectedly")
			}
		})
	}
}

func TestApplyRuntimeConfigOverrides(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create runtime config
	config := runtimeconfig.New()
	err := config.SetFromString("system:gateway:addr=8090")
	if err != nil {
		t.Fatalf("Failed to create runtime config: %v", err)
	}

	cfg := appConfig{
		runtimeConfig: config,
	}
	app := &App{
		logger: logger,
		config: cfg,
	}

	// Create test entries
	entries := []regapi.Entry{
		{
			ID:   regapi.ParseID("system:gateway"),
			Kind: "http.service",
			Data: payload.New(map[string]interface{}{
				"addr": ":8086",
				"kind": "http.service",
				"name": "gateway",
				"meta": map[string]interface{}{
					"comment": "Main HTTP gateway service",
				},
			}),
		},
		{
			ID:   regapi.ParseID("system:api"),
			Kind: "http.router",
			Data: payload.New(map[string]interface{}{
				"prefix": "/api/v1",
			}),
		},
	}

	// Apply runtime config overrides
	result, err := app.applyRuntimeConfigOverrides(entries)
	if err != nil {
		t.Fatalf("applyRuntimeConfigOverrides() error = %v", err)
	}

	// Verify gateway entry was updated
	gatewayFound := false
	for _, entry := range result {
		if entry.ID.NS == "system" && entry.ID.Name == "gateway" {
			gatewayFound = true
			data := entry.Data.Data()
			if data == nil {
				t.Errorf("Gateway entry data is nil after override")
				continue
			}

			dataMap, ok := data.(map[string]interface{})
			if !ok {
				t.Errorf("Gateway entry data is not a map, got %T", data)
				continue
			}

			addr, exists := dataMap["addr"]
			if !exists {
				t.Errorf("Gateway entry addr field not found")
				continue
			}

			addrStr, ok := addr.(string)
			if !ok {
				t.Errorf("Gateway entry addr is not a string, got %T", addr)
				continue
			}

			if addrStr != "8090" {
				t.Errorf("Gateway entry addr = %q, want %q", addrStr, "8090")
			}

			// Verify other fields are preserved
			if name, exists := dataMap["name"]; !exists || name != "gateway" {
				t.Errorf("Gateway entry name field lost or changed")
			}
			if kind, exists := dataMap["kind"]; !exists || kind != "http.service" {
				t.Errorf("Gateway entry kind field lost or changed")
			}
		}
	}

	if !gatewayFound {
		t.Errorf("Gateway entry not found in result")
	}

	// Verify other entries are unchanged
	apiFound := false
	for _, entry := range result {
		if entry.ID.NS == "system" && entry.ID.Name == "api" {
			apiFound = true
			data := entry.Data.Data()
			if data == nil {
				t.Errorf("API entry data is nil")
				continue
			}

			dataMap, ok := data.(map[string]interface{})
			if !ok {
				t.Errorf("API entry data is not a map, got %T", data)
				continue
			}

			prefix, exists := dataMap["prefix"]
			if !exists || prefix != "/api/v1" {
				t.Errorf("API entry prefix changed or lost")
			}
		}
	}

	if !apiFound {
		t.Errorf("API entry not found in result")
	}
}

func TestApplyFieldPathToEntryWithDotsInName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	app := &App{
		logger: logger,
	}

	// Test entry with dots in name (like "agents_by_name.endpoint")
	entry := regapi.Entry{
		ID:   regapi.ParseID("app:agents_by_name.endpoint"),
		Kind: "http.endpoint",
		Data: payload.New(map[string]interface{}{
			"path": "/api/test",
		}),
		Meta: regapi.Metadata{
			"router": "system:api",
		},
	}

	// Update meta.router field
	err := app.applyFieldPathToEntry(&entry, "meta.router", "core:api")
	if err != nil {
		t.Fatalf("applyFieldPathToEntry() error = %v", err)
	}

	// Verify meta was updated
	if router, exists := entry.Meta["router"]; !exists {
		t.Errorf("Meta router field not found")
	} else if routerStr, ok := router.(string); !ok || routerStr != "core:api" {
		t.Errorf("Meta router = %v, want %q", router, "core:api")
	}

	// Verify data is preserved
	data := entry.Data.Data()
	if data == nil {
		t.Errorf("Entry data is nil after update")
	} else {
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			t.Errorf("Entry data is not a map, got %T", data)
		} else if path, exists := dataMap["path"]; !exists || path != "/api/test" {
			t.Errorf("Entry data path changed or lost")
		}
	}
}
