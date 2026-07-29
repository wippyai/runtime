// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

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
	}})
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

	data, err := os.ReadFile(filepath.Join(root, "npm", "@example", "package", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "export {}" {
		t.Fatalf("materialized content = %q", data)
	}
}

func writeDependencyArtifactWAPP(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = wapp.NewWriter().PackWithResources(
		wapp.Metadata{"version": "1.0.0"},
		nil,
		[]wapp.ResourceSpec{{
			ID: wapp.NewID("example.package", "artifact"),
			Meta: wapp.Metadata{
				"artifact": map[string]any{"format": "node-package"},
			},
			FS: fstest.MapFS{
				"package.json": &fstest.MapFile{Data: []byte(
					`{"name":"@example/package","version":"1.0.0"}`,
				)},
				"dist/index.js": &fstest.MapFile{Data: []byte("export {}")},
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
