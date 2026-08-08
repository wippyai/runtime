// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wippyai/wapp"
)

type testFormat struct {
	name       string
	root       string
	descriptor Descriptor
}

func (f testFormat) Name() string { return f.name }
func (f testFormat) Root() string {
	if f.root == "" {
		return "artifacts"
	}
	return f.root
}

func (f testFormat) Inspect(context.Context, InspectInput) (Descriptor, error) {
	return f.descriptor, nil
}

func TestParseDeclaration(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		_, declared, err := ParseDeclaration(wapp.Metadata{"comment": "ordinary filesystem"})
		if err != nil || declared {
			t.Fatalf("declared=%v err=%v, want false nil", declared, err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		got, declared, err := ParseDeclaration(wapp.Metadata{
			"artifact": map[string]any{"format": " node-package "},
		})
		if err != nil || !declared || got.Format != "node-package" {
			t.Fatalf("got=%+v declared=%v err=%v", got, declared, err)
		}
	})

	for name, meta := range map[string]wapp.Metadata{
		"not object":     {"artifact": "node-package"},
		"missing format": {"artifact": map[string]any{}},
		"empty format":   {"artifact": map[string]any{"format": ""}},
		"wrong type":     {"artifact": map[string]any{"format": 1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, declared, err := ParseDeclaration(meta)
			if err == nil || !declared {
				t.Fatalf("declared=%v err=%v, want declared error", declared, err)
			}
		})
	}
}

func TestRegistryExplicitRegistration(t *testing.T) {
	registry := NewRegistry()
	format := testFormat{
		name: "test",
		descriptor: Descriptor{
			Identity:     "identity",
			Version:      "1.0.0",
			RelativePath: "artifacts/identity",
		},
	}
	if err := registry.Register(format); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(format); !errors.Is(err, ErrDuplicateFormat) {
		t.Fatalf("duplicate error = %v", err)
	}

	got, err := registry.Inspect(context.Background(), Declaration{Format: "test"}, InspectInput{
		Filesystem: fstest.MapFS{"file": &fstest.MapFile{Data: []byte("data")}},
		ResourceID: wapp.NewID("acme", "resource"),
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if got.Identity != "identity" {
		t.Fatalf("identity = %q", got.Identity)
	}

	_, err = registry.Inspect(
		context.Background(),
		Declaration{Format: "missing"},
		InspectInput{ResourceID: wapp.NewID("acme", "missing")},
	)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("unknown error = %v", err)
	}
	if !strings.Contains(err.Error(), "acme:missing") {
		t.Fatalf("unknown error lacks resource ID: %v", err)
	}
}

func TestInspectResourcesRejectsDestinationCollision(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testFormat{
		name: "test",
		descriptor: Descriptor{
			Identity:     "same",
			RelativePath: "artifacts/same",
		},
	}); err != nil {
		t.Fatal(err)
	}

	meta := wapp.Metadata{"artifact": map[string]any{"format": "test"}}
	_, err := InspectResources(context.Background(), registry, []wapp.ResourceSpec{
		{ID: wapp.NewID("acme", "one"), Meta: meta, FS: fstest.MapFS{}},
		{ID: wapp.NewID("acme", "two"), Meta: meta, FS: fstest.MapFS{}},
	}, "")
	if err == nil {
		t.Fatal("expected destination collision")
	}
}

func TestRegistryRejectsOverlappingOwnedRoots(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testFormat{name: "one", root: "generated"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(testFormat{name: "two", root: "generated/nested"}); err == nil {
		t.Fatal("expected overlapping root error")
	}
	if err := registry.Register(testFormat{name: "three", root: "other"}); err != nil {
		t.Fatalf("register non-overlapping root: %v", err)
	}
}

func TestRegistryRejectsDescriptorOutsideOwnedRoot(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testFormat{
		name: "test",
		root: "generated",
		descriptor: Descriptor{
			Identity:     "outside",
			RelativePath: "other/outside",
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := registry.Inspect(
		context.Background(),
		Declaration{Format: "test"},
		InspectInput{ResourceID: wapp.NewID("example", "outside")},
	)
	if err == nil {
		t.Fatal("expected descriptor root error")
	}
}

func TestInspectResourcesRejectsNonRegularArtifactTree(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testFormat{
		name: "test",
		root: "artifacts",
		descriptor: Descriptor{
			Identity:     "unsafe",
			RelativePath: "artifacts/unsafe",
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := InspectResources(
		context.Background(),
		registry,
		[]wapp.ResourceSpec{{
			ID:   wapp.NewID("example", "unsafe"),
			Meta: wapp.Metadata{"artifact": map[string]any{"format": "test"}},
			FS: fstest.MapFS{
				"link": &fstest.MapFile{Mode: os.ModeSymlink},
			},
		}},
		"",
	)
	if err == nil {
		t.Fatal("expected non-regular artifact tree error")
	}
}
