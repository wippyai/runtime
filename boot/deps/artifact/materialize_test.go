// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/wippyai/wapp"
)

func TestMaterializeCreatesExactMirror(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "npm", "@example", "ui")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry()
	if err := registry.Register(testFormat{
		name: "test",
		root: "npm",
		descriptor: Descriptor{
			Identity:     "@example/ui",
			Version:      "1.0.0",
			RelativePath: "npm/@example/ui",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{
		"package.json":  &fstest.MapFile{Data: []byte(`{"name":"@example/ui"}`)},
		"dist/index.js": &fstest.MapFile{Data: []byte("export {}")},
	}
	_, gotDestination, err := materializeTestResource(
		context.Background(),
		registry,
		Declaration{Format: "test"},
		InspectInput{
			Filesystem: source,
			ResourceID: wapp.NewID("example.ui", "package"),
		},
		root,
	)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if gotDestination != destination {
		t.Fatalf("destination = %q, want %q", gotDestination, destination)
	}
	if _, err := os.Stat(filepath.Join(destination, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file remains: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "export {}" {
		t.Fatalf("content = %q", data)
	}
}

func TestMaterializeRejectsNonPortableResourcePaths(t *testing.T) {
	for name, source := range map[string]fstest.MapFS{
		"reserved name": {
			"CON": &fstest.MapFile{Data: []byte("reserved")},
		},
		"case collision": {
			"dist/index.js": &fstest.MapFile{Data: []byte("one")},
			"dist/INDEX.js": &fstest.MapFile{Data: []byte("two")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry := NewRegistry()
			if err := registry.Register(testFormat{
				name: "test",
				root: "artifacts",
				descriptor: Descriptor{
					Identity:     "example",
					RelativePath: "artifacts/example",
				},
			}); err != nil {
				t.Fatal(err)
			}
			_, _, err := materializeTestResource(
				context.Background(),
				registry,
				Declaration{Format: "test"},
				InspectInput{Filesystem: source, ResourceID: wapp.NewID("example", "bad")},
				t.TempDir(),
			)
			if err == nil {
				t.Fatal("expected non-portable resource path error")
			}
		})
	}
}

func TestMaterializeRejectsEscapingFormatPath(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testFormat{
		name: "test",
		root: "artifacts",
		descriptor: Descriptor{
			Identity:     "escape",
			RelativePath: "../escape",
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := materializeTestResource(
		context.Background(),
		registry,
		Declaration{Format: "test"},
		InspectInput{Filesystem: fstest.MapFS{}, ResourceID: wapp.NewID("acme", "bad")},
		t.TempDir(),
	)
	if err == nil {
		t.Fatal("expected escaping path error")
	}
}

func TestMaterializeRejectsSymlinkedDestinationParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "npm")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(testFormat{
		name: "test",
		root: "npm",
		descriptor: Descriptor{
			Identity:     "package",
			RelativePath: "npm/package",
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := materializeTestResource(
		context.Background(),
		registry,
		Declaration{Format: "test"},
		InspectInput{
			Filesystem: fstest.MapFS{
				"package.json": &fstest.MapFile{Data: []byte("{}")},
			},
			ResourceID: wapp.NewID("example", "package"),
		},
		root,
	)
	if err == nil {
		t.Fatal("expected symlinked destination parent error")
	}
	if _, err := os.Stat(filepath.Join(outside, "package")); !os.IsNotExist(err) {
		t.Fatalf("materialized outside root: %v", err)
	}
}

func materializeTestResource(
	ctx context.Context,
	registry *Registry,
	declaration Declaration,
	input InspectInput,
	root string,
) (Descriptor, string, error) {
	effect, err := NewPartialEffect(registry, nil, []Resource{{
		Filesystem:    input.Filesystem,
		Meta:          wapp.Metadata{MetadataKey: map[string]any{"format": declaration.Format}},
		ModuleVersion: input.ModuleVersion,
		ResourceID:    input.ResourceID,
	}}, root)
	if err != nil {
		return Descriptor{}, "", err
	}
	if err := effect.Prepare(ctx); err != nil {
		return Descriptor{}, "", err
	}
	if err := effect.Commit(ctx); err != nil {
		return Descriptor{}, "", errors.Join(err, effect.Rollback(ctx))
	}
	results := effect.Results()
	if len(results) != 1 {
		return Descriptor{}, "", errors.Join(
			fmt.Errorf("materialized %d artifacts, expected 1", len(results)),
			effect.Rollback(ctx),
		)
	}
	if err := effect.Finalize(ctx); err != nil {
		return Descriptor{}, "", err
	}
	return results[0].Descriptor, results[0].Destination, nil
}
