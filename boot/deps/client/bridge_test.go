package client

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	transcoder "github.com/wippyai/runtime/system/payload"
	jpayload "github.com/wippyai/runtime/system/payload/json"
	ypayload "github.com/wippyai/runtime/system/payload/yaml"
)

func TestExtractDependenciesFromEntries(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	ypayload.Register(dtt)

	t.Run("extracts dependencies from ns.dependency entries", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ID{NS: "test", Name: "dep1"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]any{
					"component": "acme/http",
					"version":   "^1.0.0",
				}),
			},
			{
				ID:   registry.ID{NS: "test", Name: "dep2"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]any{
					"component": "demo/sql",
					"version":   "~2.0.0",
					"params":    map[string]any{"driver": "postgres"},
				}),
			},
		}

		deps, err := extractDependenciesFromEntries(entries, dtt)
		if err != nil {
			t.Fatalf("extractDependenciesFromEntries failed: %v", err)
		}

		if len(deps) != 2 {
			t.Fatalf("expected 2 dependencies, got %d", len(deps))
		}

		if deps[0].Name.String() != "acme/http" {
			t.Errorf("expected acme/http, got %s", deps[0].Name.String())
		}
		if deps[0].Version != "^1.0.0" {
			t.Errorf("expected ^1.0.0, got %s", deps[0].Version)
		}

		if deps[1].Name.String() != "demo/sql" {
			t.Errorf("expected demo/sql, got %s", deps[1].Name.String())
		}
		if deps[1].Version != "~2.0.0" {
			t.Errorf("expected ~2.0.0, got %s", deps[1].Version)
		}
		if deps[1].Parameters["driver"] != "postgres" {
			t.Errorf("expected postgres driver param, got %v", deps[1].Parameters)
		}
	})

	t.Run("ignores non-dependency entries", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ID{NS: "test", Name: "service"},
				Kind: "service",
				Data: payload.New(map[string]any{"component": "acme/http"}),
			},
			{
				ID:   registry.ID{NS: "test", Name: "dep1"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]any{"component": "acme/http", "version": "^1.0.0"}),
			},
		}

		deps, err := extractDependenciesFromEntries(entries, dtt)
		if err != nil {
			t.Fatalf("extractDependenciesFromEntries failed: %v", err)
		}

		if len(deps) != 1 {
			t.Fatalf("expected 1 dependency, got %d", len(deps))
		}
	})

	t.Run("skips entries with empty component", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ID{NS: "test", Name: "dep1"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]any{"version": "^1.0.0"}),
			},
		}

		deps, err := extractDependenciesFromEntries(entries, dtt)
		if err != nil {
			t.Fatalf("extractDependenciesFromEntries failed: %v", err)
		}

		if len(deps) != 0 {
			t.Fatalf("expected 0 dependencies, got %d", len(deps))
		}
	})

	t.Run("skips entries with invalid component format", func(t *testing.T) {
		entries := []registry.Entry{
			{
				ID:   registry.ID{NS: "test", Name: "dep1"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]any{"component": "invalid", "version": "^1.0.0"}),
			},
			{
				ID:   registry.ID{NS: "test", Name: "dep2"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]any{"component": "acme/http", "version": "^1.0.0"}),
			},
		}

		deps, err := extractDependenciesFromEntries(entries, dtt)
		if err != nil {
			t.Fatalf("extractDependenciesFromEntries failed: %v", err)
		}

		if len(deps) != 1 {
			t.Fatalf("expected 1 dependency, got %d", len(deps))
		}
		if deps[0].Name.String() != "acme/http" {
			t.Errorf("expected acme/http, got %s", deps[0].Name.String())
		}
	})

	t.Run("handles empty entry list", func(t *testing.T) {
		deps, err := extractDependenciesFromEntries([]registry.Entry{}, dtt)
		if err != nil {
			t.Fatalf("extractDependenciesFromEntries failed: %v", err)
		}

		if len(deps) != 0 {
			t.Fatalf("expected 0 dependencies, got %d", len(deps))
		}
	})
}

func TestNewManifestBridge(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	ypayload.Register(dtt)

	t.Run("creates bridge with default cache size", func(t *testing.T) {
		bridge, err := NewManifestBridge(nil, dtt, nil, 0)
		if err != nil {
			t.Fatalf("NewManifestBridge failed: %v", err)
		}
		if bridge == nil {
			t.Fatal("expected non-nil bridge")
		}
	})

	t.Run("creates bridge with custom cache size", func(t *testing.T) {
		bridge, err := NewManifestBridge(nil, dtt, nil, 50)
		if err != nil {
			t.Fatalf("NewManifestBridge failed: %v", err)
		}
		if bridge == nil {
			t.Fatal("expected non-nil bridge")
		}
	})
}

func TestManifestBridge_FetchManifests(t *testing.T) {
	t.Run("returns nil for empty requests", func(t *testing.T) {
		dtt := transcoder.NewTranscoder()
		jpayload.Register(dtt)
		ypayload.Register(dtt)
		bridge, err := NewManifestBridge(nil, dtt, nil, 10)
		if err != nil {
			t.Fatalf("NewManifestBridge failed: %v", err)
		}

		result, err := bridge.FetchManifests(context.Background(), []graph.ManifestRequest{})
		if err != nil {
			t.Fatalf("FetchManifests failed: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})
}
