// SPDX-License-Identifier: MPL-2.0

package artifact_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/artifact/nodepackage"
	"github.com/wippyai/wapp"
)

func TestWAPPEffectRollbackRestoresPreviousOutput(t *testing.T) {
	root := t.TempDir()
	packPath := filepath.Join(t.TempDir(), "package.wapp")
	writeArtifactWAPP(t, packPath, "@example/package", "1.0.0", "new")

	destination := filepath.Join(root, "npm", "@example", "package")
	if err := os.MkdirAll(filepath.Join(destination, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "dist", "index.js"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	effect, err := artifact.NewWAPPEffect(testArtifactRegistry(t), []artifact.WAPP{{
		Path:          packPath,
		ModuleVersion: "1.0.0",
	}}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := effect.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	assertFileContent(t, filepath.Join(destination, "dist", "index.js"), "new")
	if err := effect.Rollback(context.Background()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	assertFileContent(t, filepath.Join(destination, "dist", "index.js"), "old")
}

func TestWAPPEffectCommitKeepsOutputAndRemovesBackup(t *testing.T) {
	root := t.TempDir()
	packPath := filepath.Join(t.TempDir(), "package.wapp")
	writeArtifactWAPP(t, packPath, "@example/package", "1.0.0", "new")

	destination := filepath.Join(root, "npm", "@example", "package")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	effect, err := artifact.NewWAPPEffect(testArtifactRegistry(t), []artifact.WAPP{{
		Path: packPath,
	}}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := effect.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := effect.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := effect.Finalize(context.Background()); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	assertFileContent(t, filepath.Join(destination, "dist", "index.js"), "new")
	if _, err := os.Stat(filepath.Join(destination, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale output remains: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".package.artifact-backup-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("artifact backups remain: %v", backups)
	}
}

func TestWAPPEffectRejectsPackCollisionBeforeMutation(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(t.TempDir(), "first.wapp")
	second := filepath.Join(t.TempDir(), "second.wapp")
	writeArtifactWAPP(t, first, "@example/package", "1.0.0", "first")
	writeArtifactWAPP(t, second, "@example/package", "1.0.0", "second")

	destination := filepath.Join(root, "npm", "@example", "package", "dist", "index.js")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	effect, err := artifact.NewWAPPEffect(testArtifactRegistry(t), []artifact.WAPP{
		{Path: first},
		{Path: second},
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := effect.Prepare(context.Background()); err == nil {
		t.Fatal("expected destination collision")
	}
	assertFileContent(t, destination, "old")
}

func testArtifactRegistry(t *testing.T) *artifact.Registry {
	t.Helper()
	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		t.Fatal(err)
	}
	return registry
}

func writeArtifactWAPP(t *testing.T, path, packageName, version, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	resourceID := wapp.NewID("example.package", "artifact")
	err = wapp.NewWriter().PackWithResources(
		wapp.Metadata{"version": version},
		nil,
		[]wapp.ResourceSpec{{
			ID: resourceID,
			Meta: wapp.Metadata{
				"artifact": map[string]any{"format": "node-package"},
			},
			FS: fstest.MapFS{
				"package.json": &fstest.MapFile{Data: []byte(
					`{"name":"` + packageName + `","version":"` + version + `"}`,
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

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
