// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/artifact/nodepackage"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestBuildArtifactEffectMaterializesVerifiedResolvedWAPP(t *testing.T) {
	root := t.TempDir()
	vendorDir := filepath.Join(root, "vendor")
	name := graph.Name{Organization: "example", Module: "package"}
	packPath := filepath.Join(vendorDir, lock.WappPath(name, "1.0.0"))
	writeDependencyArtifactWAPP(t, packPath)

	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		t.Fatal(err)
	}
	handler := &DependencyHandler{
		artifacts:    registry,
		artifactRoot: root,
		vendorDir:    vendorDir,
		replacements: map[string]lock.Replacement{},
		logger:       zap.NewNop(),
	}

	effect, err := handler.buildArtifactEffect(context.Background(), []ResolvedModule{{
		Org:     name.Organization,
		Name:    name.Module,
		Version: "1.0.0",
	}}, nil)
	if err != nil {
		t.Fatalf("build artifact effect: %v", err)
	}
	if effect == nil {
		t.Fatal("expected artifact effect")
	}
	if err := effect.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := effect.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if finalizer, ok := effect.(regapi.FinalizingEffect); ok {
		if err := finalizer.Finalize(context.Background()); err != nil {
			t.Fatalf("finalize: %v", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "npm", "@example", "package", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "export {}" {
		t.Fatalf("materialized content = %q", data)
	}
}

func TestBuildArtifactEffectMaterializesLocalReplacement(t *testing.T) {
	root := t.TempDir()
	replacement := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(replacement, "package.json"),
		[]byte(`{"name":"@example/package","version":"1.0.0"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(replacement, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "dist", "index.js"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		t.Fatal(err)
	}
	handler := &DependencyHandler{
		artifacts:    registry,
		artifactRoot: root,
		replacements: map[string]lock.Replacement{
			"example/package": {From: "example/package", To: replacement},
		},
		logger: zap.NewNop(),
	}
	state := regapi.State{{
		ID:       regapi.NewID("example.package", "artifact"),
		Kind:     dirapi.Kind,
		Registry: regapi.EntryMetadata{Owner: "example/package"},
		Meta: attrs.NewBagFrom(map[string]any{
			"artifact": map[string]any{"format": "node-package"},
		}),
		Data: payload.New(map[string]any{
			"directory": ".",
			"base":      dirapi.BaseModule,
		}),
	}}
	effect, err := handler.buildArtifactEffect(newTestContext(), []ResolvedModule{{
		Org:     "example",
		Name:    "package",
		Version: "1.0.0",
		Source:  moduleSourceReplacementTreeV1,
	}}, state)
	if err != nil {
		t.Fatalf("build artifact effect: %v", err)
	}
	if err := effect.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := effect.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if finalizer, ok := effect.(regapi.FinalizingEffect); ok {
		if err := finalizer.Finalize(context.Background()); err != nil {
			t.Fatalf("finalize: %v", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "npm", "@example", "package", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "local" {
		t.Fatalf("materialized content = %q", data)
	}
}

func TestBuildArtifactEffectRemovesOutputsWhenGraphIsEmpty(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "npm", "@example", "removed", "index.js")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		t.Fatal(err)
	}
	handler := &DependencyHandler{
		artifacts:    registry,
		artifactRoot: root,
		replacements: map[string]lock.Replacement{},
		logger:       zap.NewNop(),
	}
	effect, err := handler.buildArtifactEffect(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if effect == nil {
		t.Fatal("expected exact reconciliation effect")
	}
	if err := effect.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := effect.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if finalizer, ok := effect.(regapi.FinalizingEffect); ok {
		if err := finalizer.Finalize(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("removed artifact remains: %v", err)
	}
}

func TestBuildArtifactEffectReconcilesInstallUpdateAndUninstall(t *testing.T) {
	root := t.TempDir()
	vendorDir := filepath.Join(root, "vendor")
	name := graph.Name{Organization: "example", Module: "package"}
	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		t.Fatal(err)
	}
	handler := &DependencyHandler{
		artifacts:    registry,
		artifactRoot: root,
		vendorDir:    vendorDir,
		replacements: map[string]lock.Replacement{},
		logger:       zap.NewNop(),
	}
	apply := func(resolved []ResolvedModule) {
		t.Helper()
		effect, err := handler.buildArtifactEffect(context.Background(), resolved, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := effect.Prepare(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := effect.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if finalizer, ok := effect.(regapi.FinalizingEffect); ok {
			if err := finalizer.Finalize(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	output := filepath.Join(root, "npm", "@example", "package", "dist", "index.js")

	for _, version := range []string{"1.0.0", "1.1.0"} {
		packPath := filepath.Join(vendorDir, lock.WappPath(name, version))
		writeDependencyArtifactWAPPVersion(t, packPath, version, version)
		apply([]ResolvedModule{{
			Org: name.Organization, Name: name.Module, Version: version,
		}})
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != version {
			t.Fatalf("materialized version = %q, want %q", data, version)
		}
	}

	apply(nil)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("uninstalled artifact remains: %v", err)
	}
}

func writeDependencyArtifactWAPP(t *testing.T, path string) {
	writeDependencyArtifactWAPPVersion(t, path, "1.0.0", "export {}")
}

func writeDependencyArtifactWAPPVersion(t *testing.T, path, version, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = wapp.NewWriter().PackWithResources(
		wapp.Metadata{"version": version},
		nil,
		[]wapp.ResourceSpec{{
			ID: wapp.NewID("example.package", "artifact"),
			Meta: wapp.Metadata{
				"artifact": map[string]any{"format": "node-package"},
			},
			FS: fstest.MapFS{
				"package.json": &fstest.MapFile{Data: []byte(
					`{"name":"@example/package","version":"` + version + `"}`,
				)},
				"dist/index.js": &fstest.MapFile{Data: []byte(content)},
			},
		}},
		file,
	)
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
