// SPDX-License-Identifier: MPL-2.0

package wappextract

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wippyai/wapp"
	"gopkg.in/yaml.v3"
)

func TestCommonDotPrefix(t *testing.T) {
	tests := []struct {
		name string
		want string
		in   []string
	}{
		{name: "empty", in: nil, want: ""},
		{name: "single", in: []string{"app.test"}, want: "app.test"},
		{name: "shared prefix", in: []string{"app.test.one", "app.test.two"}, want: "app.test"},
		{name: "no shared prefix", in: []string{"app.one", "lib.two"}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commonDotPrefix(tt.in)
			if got != tt.want {
				t.Fatalf("commonDotPrefix(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveNamespaceDirs(t *testing.T) {
	root := filepath.Join("tmp", "extract")

	tests := []struct {
		want       map[string]string
		name       string
		namespaces []string
	}{
		{
			name:       "single namespace stays at root",
			namespaces: []string{"app.test"},
			want: map[string]string{
				"app.test": root,
			},
		},
		{
			name:       "shared prefix maps to suffix dirs",
			namespaces: []string{"app.test.one", "app.test.two"},
			want: map[string]string{
				"app.test.one": filepath.Join(root, "one"),
				"app.test.two": filepath.Join(root, "two"),
			},
		},
		{
			name:       "no prefix creates full namespace paths",
			namespaces: []string{"app.one", "lib.two"},
			want: map[string]string{
				"app.one": filepath.Join(root, "app", "one"),
				"lib.two": filepath.Join(root, "lib", "two"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveNamespaceDirs(root, tt.namespaces)
			if err != nil {
				t.Fatalf("resolveNamespaceDirs failed: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("resolveNamespaceDirs returned %d dirs, want %d", len(got), len(tt.want))
			}
			for ns, wantDir := range tt.want {
				if got[ns] != wantDir {
					t.Fatalf("resolveNamespaceDirs(%q) = %q, want %q", ns, got[ns], wantDir)
				}
			}
		})
	}
}

func TestResolveNamespaceDirsRejectsUnsafeNamespaceSegment(t *testing.T) {
	_, err := resolveNamespaceDirs(t.TempDir(), []string{"app.good", "app.bad/evil"})
	if err == nil {
		t.Fatal("expected unsafe namespace segment to fail")
	}
}

func TestSourceExtForKind(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want string
	}{
		{name: "exact function lua", kind: "function.lua", want: ".lua"},
		{name: "exact template jet", kind: "template.jet", want: ".jet"},
		{name: "suffix fallback lua", kind: "custom.lua", want: ".lua"},
		{name: "suffix fallback jet", kind: "custom.jet", want: ".jet"},
		{name: "unsupported kind", kind: "config.yaml", want: ""},
		{name: "no extension", kind: "config", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceExtForKind(tt.kind)
			if got != tt.want {
				t.Fatalf("sourceExtForKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestExtractWappToDirRestoresEmbeddedFilesystem(t *testing.T) {
	projectRoot := t.TempDir()
	vendorDir := filepath.Join(projectRoot, ".wippy", "vendor", "acme")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}

	wappPath := filepath.Join(vendorDir, "ui-v1.0.0.wapp")
	writeTestWapp(t, wappPath, []wapp.Entry{{
		ID:   wapp.NewID("acme.ui", "static_fs"),
		Kind: "fs.embed",
		Meta: wapp.Metadata{"module": "acme/ui"},
		Data: map[string]any{},
	}}, []wapp.ResourceSpec{{
		ID: wapp.NewID("acme.ui", "static_fs"),
		FS: fstest.MapFS{
			"app.js": &fstest.MapFile{Data: []byte("export const ok = true;\n"), Mode: 0644},
		},
	}})

	targetDir := filepath.Join(vendorDir, "ui")
	if err := ExtractWappToDir(wappPath, targetDir); err != nil {
		t.Fatalf("ExtractWappToDir failed: %v", err)
	}

	if _, err := os.Stat(wappPath); err == nil {
		t.Fatalf("packed file should be removed after extraction: %s", wappPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat packed file: %v", err)
	}

	extractedJS := filepath.Join(targetDir, "static_fs", "app.js")
	data, err := os.ReadFile(extractedJS)
	if err != nil {
		t.Fatalf("read extracted app.js: %v", err)
	}
	if string(data) != "export const ok = true;\n" {
		t.Fatalf("extracted app.js = %q", string(data))
	}

	var index struct {
		Entries []struct {
			Name      string `yaml:"name"`
			Kind      string `yaml:"kind"`
			Directory string `yaml:"directory"`
			Base      string `yaml:"base"`
		} `yaml:"entries"`
	}
	indexData, err := os.ReadFile(filepath.Join(targetDir, "_index.yaml"))
	if err != nil {
		t.Fatalf("read extracted index: %v", err)
	}
	if err := yaml.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("parse extracted index: %v", err)
	}

	for _, entry := range index.Entries {
		if entry.Name != "static_fs" {
			continue
		}
		if entry.Kind != "fs.directory" || entry.Directory != "static_fs" || entry.Base != "module" {
			t.Fatalf("restored entry = %+v", entry)
		}
		return
	}
	t.Fatal("static_fs entry not found in extracted index")
}

func TestExtractWappToDirKeepSourcePreservesPack(t *testing.T) {
	dir := t.TempDir()
	wappPath := filepath.Join(dir, "mod.wapp")
	writeTestWapp(t, wappPath, []wapp.Entry{{
		ID:   wapp.NewID("app", "svc"),
		Kind: "service",
		Data: map[string]any{"ok": true},
	}}, nil)

	targetDir := filepath.Join(dir, "mod")
	if err := ExtractWappToDirKeepSource(wappPath, targetDir); err != nil {
		t.Fatalf("ExtractWappToDirKeepSource failed: %v", err)
	}
	if _, err := os.Stat(wappPath); err != nil {
		t.Fatalf("source pack should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "_index.yaml")); err != nil {
		t.Fatalf("extracted index missing: %v", err)
	}
}

func TestExtractWappToDirRejectsUnsafeEntryName(t *testing.T) {
	dir := t.TempDir()
	wappPath := filepath.Join(dir, "mod.wapp")
	writeTestWapp(t, wappPath, []wapp.Entry{{
		ID:   wapp.NewID("app", "../escape"),
		Kind: "function.lua",
		Data: map[string]any{"source": "return true"},
	}}, nil)

	targetDir := filepath.Join(dir, "mod")
	err := ExtractWappToDir(wappPath, targetDir)
	if err == nil {
		t.Fatal("expected unsafe entry name to fail")
	}
	if _, statErr := os.Stat(wappPath); statErr != nil {
		t.Fatalf("source pack should remain on extraction failure: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "escape.lua")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe source path should not be written, stat err = %v", statErr)
	}
}

func TestSafeResourcePathRejectsUnsafePaths(t *testing.T) {
	targetDir := t.TempDir()
	tests := []string{
		"../escape.txt",
		"dir/../../escape.txt",
		"/absolute.txt",
		`dir\escape.txt`,
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if _, err := safeResourcePath(targetDir, path); err == nil {
				t.Fatalf("expected %q to be rejected", path)
			}
		})
	}
}

func TestExtractResourceFSClosesOpenedFiles(t *testing.T) {
	targetDir := t.TempDir()
	resFS := &trackingResourceFS{data: bytes.Repeat([]byte("x"), 1024)}

	if err := extractResourceFS(targetDir, resFS); err != nil {
		t.Fatalf("extractResourceFS failed: %v", err)
	}
	if resFS.opens != 1 {
		t.Fatalf("opens = %d, want 1", resFS.opens)
	}
	if resFS.closes != 1 {
		t.Fatalf("closes = %d, want 1", resFS.closes)
	}
	if data, err := os.ReadFile(filepath.Join(targetDir, "asset.bin")); err != nil {
		t.Fatalf("read extracted asset: %v", err)
	} else if !bytes.Equal(data, resFS.data) {
		t.Fatalf("extracted asset mismatch")
	}
}

func writeTestWapp(t *testing.T, path string, entries []wapp.Entry, resources []wapp.ResourceSpec) {
	t.Helper()

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	var err error
	if len(resources) > 0 {
		err = writer.PackWithResources(wapp.Metadata{}, entries, resources, &buf)
	} else {
		err = writer.PackEntries(wapp.Metadata{}, entries, &buf)
	}
	if err != nil {
		t.Fatalf("pack test wapp: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatalf("write pack: %v", err)
	}
}

type trackingResourceFS struct {
	data   []byte
	opens  int
	closes int
}

func (f *trackingResourceFS) Open(name string) (fs.File, error) {
	if name == "." {
		return trackingDirFile{}, nil
	}
	if name != "asset.bin" {
		return nil, fs.ErrNotExist
	}
	f.opens++
	return &trackingResourceFile{
		reader: bytes.NewReader(f.data),
		owner:  f,
	}, nil
}

func (f *trackingResourceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, fs.ErrNotExist
	}
	return []fs.DirEntry{trackingDirEntry{name: "asset.bin"}}, nil
}

type trackingDirFile struct{}

func (f trackingDirFile) Stat() (fs.FileInfo, error) {
	return trackingFileInfo{name: ".", mode: fs.ModeDir | 0755}, nil
}

func (f trackingDirFile) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (f trackingDirFile) Close() error {
	return nil
}

type trackingResourceFile struct {
	reader *bytes.Reader
	owner  *trackingResourceFS
}

func (f *trackingResourceFile) Stat() (fs.FileInfo, error) {
	return trackingFileInfo{name: "asset.bin", size: int64(f.reader.Len()), mode: 0644}, nil
}

func (f *trackingResourceFile) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

func (f *trackingResourceFile) Close() error {
	f.owner.closes++
	return nil
}

var _ io.Reader = (*trackingResourceFile)(nil)

type trackingDirEntry struct {
	name string
}

func (e trackingDirEntry) Name() string {
	return e.name
}

func (e trackingDirEntry) IsDir() bool {
	return false
}

func (e trackingDirEntry) Type() fs.FileMode {
	return 0
}

func (e trackingDirEntry) Info() (fs.FileInfo, error) {
	return trackingFileInfo{name: e.name, size: 1024, mode: 0644}, nil
}

type trackingFileInfo struct {
	name string
	mode fs.FileMode
	size int64
}

func (i trackingFileInfo) Name() string {
	return i.name
}

func (i trackingFileInfo) Size() int64 {
	return i.size
}

func (i trackingFileInfo) Mode() fs.FileMode {
	return i.mode
}

func (i trackingFileInfo) ModTime() time.Time {
	return time.Time{}
}

func (i trackingFileInfo) IsDir() bool {
	return i.mode.IsDir()
}

func (i trackingFileInfo) Sys() any {
	return nil
}
