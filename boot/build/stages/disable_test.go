package stages

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestDisable_ByExactNamespace(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("test", "fixture"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"test"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if e.ID.NS == "test" {
			t.Errorf("Entry from 'test' namespace should be disabled")
		}
	}
}

func TestDisable_ByNamespaceWildcard(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app.v1", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app.v2", "cache"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"app.**"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID.NS != "db" {
		t.Errorf("Expected only 'db' namespace to remain, got %s", entries[0].ID.NS)
	}
}

func TestDisable_ByExactEntryID(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"entries": []string{"app:gateway"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if e.ID.NS == "app" && e.ID.Name == "gateway" {
			t.Errorf("Entry 'app:gateway' should be disabled")
		}
	}
}

func TestDisable_ByEntryWildcard(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway.v1"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app", "gateway.v2"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"entries": []string{"app:gateway.*"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID.Name != "worker" {
		t.Errorf("Expected 'worker' to remain, got %s", entries[0].ID.Name)
	}
}

func TestDisable_ByEntryWildcardNamespace(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.v1", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db.v1", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app.v2", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"entries": []string{"*.v1:gateway"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID.NS != "app.v2" {
		t.Errorf("Expected 'app.v2' to remain, got %s", entries[0].ID.NS)
	}
}

func TestDisable_MultipleNamespaces(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("cache", "redis"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("queue", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"app", "db", "queue"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID.NS != "cache" {
		t.Errorf("Expected 'cache' namespace to remain, got %s", entries[0].ID.NS)
	}
}

func TestDisable_MultipleEntries(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "cache"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"entries": []string{"app:gateway", "db:cache"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if (e.ID.NS == "app" && e.ID.Name == "gateway") || (e.ID.NS == "db" && e.ID.Name == "cache") {
			t.Errorf("Entry %s:%s should be disabled", e.ID.NS, e.ID.Name)
		}
	}
}

func TestDisable_BothNamespacesAndEntries(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("db", "main"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("cache", "redis"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"app"},
			"entries":    []string{"db:main"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID.NS != "cache" {
		t.Errorf("Expected 'cache:redis' to remain, got %s:%s", entries[0].ID.NS, entries[0].ID.Name)
	}
}

func TestDisable_NoConfig(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
}

func TestDisable_EmptyPatterns(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{},
			"entries":    []string{},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
}

func TestDisable_PatternMatchesNothing(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"nonexistent"},
			"entries":    []string{"fake:entry"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
}

func TestDisable_AllEntriesDisabled(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("app", "worker"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"app"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries, got %d", len(entries))
	}
}

func TestDisable_InvalidEntryPattern(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "gateway"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	cfg := boot.NewConfig(
		boot.WithSection("disable", map[string]any{
			"entries": []string{"invalid-no-colon"},
		}),
	)

	ctx = boot.WithConfig(ctx, cfg)
	stage := Disable()

	err := stage.Execute(ctx, &entries)
	if err == nil {
		t.Fatal("Expected error for invalid entry pattern, got nil")
	}

	if !containsString(err.Error(), "invalid entry pattern") {
		t.Errorf("Expected error about invalid entry pattern, got %v", err)
	}
}
