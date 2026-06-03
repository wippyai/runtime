// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestRunPackEntries_InvalidRequirementFailsNormalizationPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	prevConfigFile := configFile
	configFile = cfgPath
	t.Cleanup(func() {
		configFile = prevConfigFile
	})

	ctx, loader, logger, embedReg, err := bootstrapPackRuntime(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("bootstrap pack runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = embedReg.Close()
	})
	t.Cleanup(func() {
		_ = shutdown.Perform(ctx, loader, logger, true)
	})

	packEntries := []regapi.Entry{
		{
			ID:   regapi.NewID("test", "broken.requirement"),
			Kind: regapi.NamespaceRequirement,
			Data: payload.New("not-a-requirement-definition"),
		},
		{
			ID:   regapi.NewID("app", "runner"),
			Kind: "process.lua",
			Meta: map[string]any{
				"command": map[string]any{
					"name": "run",
				},
			},
			Data: payload.New(map[string]any{
				"source": "return {}",
			}),
		},
	}

	err = runPackEntries(ctx, loader, zap.NewNop(), packEntries, []string{"missing"})
	if err == nil {
		t.Fatal("expected normalization pipeline error")
	}

	errText := err.Error()
	if !strings.Contains(errText, "failed to execute pipeline") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errText, "failed to decode requirement") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFromPackFiles_InvalidRequirementFailsNormalizationPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	prevConfigFile := configFile
	configFile = cfgPath
	t.Cleanup(func() {
		configFile = prevConfigFile
	})

	packPath := createTestPackFile(t, tmpDir, "snapshot", []wapp.Entry{
		{
			ID:   wapp.NewID("test", "broken.requirement"),
			Kind: regapi.NamespaceRequirement,
			Data: "not-a-requirement-definition",
		},
		{
			ID:   wapp.NewID("app", "runner"),
			Kind: "process.lua",
			Meta: wapp.Metadata{
				"command": map[string]any{"name": "run"},
			},
			Data: map[string]any{"source": "return {}"},
		},
	})

	err := runFromPackFiles(nil, []string{packPath}, []string{"missing"})
	if err == nil {
		t.Fatal("expected command lookup error")
	}
	errText := err.Error()
	if !strings.Contains(errText, "failed to execute pipeline") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errText, "failed to decode requirement") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsProcessKind(t *testing.T) {
	tests := []struct {
		name string
		kind regapi.Kind
		want bool
	}{
		{name: "lua process", kind: "process.lua", want: true},
		{name: "lua bytecode process", kind: "process.lua.bc", want: true},
		{name: "wasm process", kind: "process.wasm", want: true},
		{name: "lua function", kind: "function.lua", want: false},
		{name: "wasm function", kind: "function.wasm", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isProcessKind(tc.kind); got != tc.want {
				t.Fatalf("isProcessKind(%q) = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

func TestIsHubModuleRef_WithUppercaseWappExtension(t *testing.T) {
	if got := isHubModuleRef("dockerio.WAPP"); got {
		t.Fatalf("isHubModuleRef returned true for .WAPP path")
	}
}

func TestSelectPackCommand(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
		wantEntry   string
		wantErr     string
		commands    []packCommand
	}{
		{name: "no commands", commands: nil, wantEntry: ""},
		{
			name:        "explicit name match",
			commands:    []packCommand{{name: "snake", entryID: "snake:play"}, {name: "edit", entryID: "snake:edit"}},
			commandName: "edit",
			wantEntry:   "snake:edit",
		},
		{
			name:        "explicit name missing",
			commands:    []packCommand{{name: "snake", entryID: "snake:play"}},
			commandName: "nope",
			wantErr:     `command "nope" not found in pack`,
		},
		{
			name:      "single command without main auto-runs",
			commands:  []packCommand{{name: "snake", entryID: "snake:play"}},
			wantEntry: "snake:play",
		},
		{
			name:      "single command with main auto-runs",
			commands:  []packCommand{{name: "snake", entryID: "snake:play", main: true}},
			wantEntry: "snake:play",
		},
		{
			name:      "multiple commands one main",
			commands:  []packCommand{{name: "snake", entryID: "snake:play"}, {name: "edit", entryID: "snake:edit", main: true}},
			wantEntry: "snake:edit",
		},
		{
			name:     "multiple commands no main errors with sorted names",
			commands: []packCommand{{name: "snake", entryID: "snake:play"}, {name: "edit", entryID: "snake:edit"}},
			wantErr:  "no command is marked as main; specify one of: edit, snake",
		},
		{
			name:     "multiple main commands error",
			commands: []packCommand{{name: "snake", entryID: "snake:play", main: true}, {name: "edit", entryID: "snake:edit", main: true}},
			wantErr:  "multiple commands marked as main in pack: snake, edit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectPackCommand(tc.commands, tc.commandName)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("selectPackCommand error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("selectPackCommand unexpected error: %v", err)
			}

			if got != tc.wantEntry {
				t.Fatalf("selectPackCommand = %q, want %q", got, tc.wantEntry)
			}
		})
	}
}

func TestBootstrapPackRuntimeWithDefaults_Harness(t *testing.T) {
	t.Run("applies runtime defaults when config key is missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "wippy.yaml")
		if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		prevConfigFile := configFile
		configFile = cfgPath
		t.Cleanup(func() {
			configFile = prevConfigFile
		})

		runtimeDefaults := boot.NewConfig(boot.WithSection("lsp", map[string]any{
			"enabled": true,
		}))

		ctx, _, _, embedReg, err := bootstrapPackRuntimeWithDefaults(nil, zap.NewNop(), runtimeDefaults)
		if err != nil {
			t.Fatalf("bootstrap pack runtime: %v", err)
		}
		t.Cleanup(func() {
			_ = embedReg.Close()
		})

		cfg := boot.GetConfig(ctx)
		if cfg == nil {
			t.Fatal("missing boot config in context")
		}
		if got := cfg.GetBool("lsp.enabled", false); !got {
			t.Fatalf("lsp.enabled = %v, want true", got)
		}
	})

	t.Run("config file overrides runtime defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "wippy.yaml")
		if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\nlsp:\n  enabled: false\n"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		prevConfigFile := configFile
		configFile = cfgPath
		t.Cleanup(func() {
			configFile = prevConfigFile
		})

		runtimeDefaults := boot.NewConfig(boot.WithSection("lsp", map[string]any{
			"enabled": true,
		}))

		ctx, _, _, embedReg, err := bootstrapPackRuntimeWithDefaults(nil, zap.NewNop(), runtimeDefaults)
		if err != nil {
			t.Fatalf("bootstrap pack runtime: %v", err)
		}
		t.Cleanup(func() {
			_ = embedReg.Close()
		})

		cfg := boot.GetConfig(ctx)
		if cfg == nil {
			t.Fatal("missing boot config in context")
		}
		if got := cfg.GetBool("lsp.enabled", true); got {
			t.Fatalf("lsp.enabled = %v, want false", got)
		}
	})
}

func TestLoadPackEntries_RawLoadSkipsLinkPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := createTestPackFile(t, tmpDir, "raw-load", []wapp.Entry{
		{
			ID:   wapp.NewID("test", "broken.requirement"),
			Kind: regapi.NamespaceRequirement,
			Data: "not-a-requirement-definition",
		},
		{
			ID:   wapp.NewID("app", "runner"),
			Kind: "process.lua",
			Meta: wapp.Metadata{
				"command": map[string]any{"name": "run"},
			},
			Data: map[string]any{"source": "return {}"},
		},
	})

	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	packEntries, err := loadPackEntries([]string{packPath}, embedReg)
	if err != nil {
		t.Fatalf("loadPackEntries failed: %v", err)
	}
	if len(packEntries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(packEntries))
	}
}

func TestLoadPackEntries_RejectsUnsupportedExtension(t *testing.T) {
	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	_, err := loadPackEntries([]string{"./not-a-pack.yaml"}, embedReg)
	if err == nil {
		t.Fatal("expected error for unsupported pack extension")
	}
	if !strings.Contains(err.Error(), `unsupported pack format "./not-a-pack.yaml"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPackEntries_AcceptsUppercaseExtension(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := createTestPackFile(t, tmpDir, "uppercase-ext", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "runner"),
			Kind: "process.lua",
			Meta: wapp.Metadata{
				"command": map[string]any{"name": "run"},
			},
			Data: map[string]any{"source": "return {}"},
		},
	})
	upperPath := filepath.Join(tmpDir, "UPPER.WAPP")
	if err := os.Rename(packPath, upperPath); err != nil {
		t.Fatalf("rename pack file: %v", err)
	}

	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	packEntries, err := loadPackEntries([]string{upperPath}, embedReg)
	if err != nil {
		t.Fatalf("loadPackEntries failed: %v", err)
	}
	if len(packEntries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(packEntries))
	}
}

func TestLoadPackEntries_RegisterErrorIncludesPath(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := createTestPackFile(t, tmpDir, "register-failure", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "runner"),
			Kind: "process.lua",
			Data: map[string]any{"source": "return {}"},
		},
	})

	_, err := loadPackEntries([]string{packPath}, failingPackRegistry{err: errors.New("boom")})
	if err == nil {
		t.Fatal("expected register error")
	}
	if !strings.Contains(err.Error(), "register embed resources for") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), packPath) {
		t.Fatalf("error does not include pack path: %v", err)
	}
}

func TestLoadPackEntries_MultiPackOrder(t *testing.T) {
	tmpDir := t.TempDir()
	depPack := createTestPackFile(t, tmpDir, "dep", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "config"),
			Kind: "state",
			Data: map[string]any{"value": "dep"},
		},
	})
	mainPack := createTestPackFile(t, tmpDir, "main", []wapp.Entry{
		{
			ID:   wapp.NewID("app", "config"),
			Kind: "state",
			Data: map[string]any{"value": "main"},
		},
	})

	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	packEntries, err := loadPackEntries([]string{depPack, mainPack}, embedReg)
	if err != nil {
		t.Fatalf("loadPackEntries failed: %v", err)
	}
	if len(packEntries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(packEntries))
	}

	first, ok := packEntries[0].Data.Data().(map[string]any)
	if !ok {
		t.Fatalf("first data type = %T, want map[string]any", packEntries[0].Data.Data())
	}
	second, ok := packEntries[1].Data.Data().(map[string]any)
	if !ok {
		t.Fatalf("second data type = %T, want map[string]any", packEntries[1].Data.Data())
	}

	if first["value"] != "dep" {
		t.Fatalf("first entry value = %v, want dep", first["value"])
	}
	if second["value"] != "main" {
		t.Fatalf("second entry value = %v, want main", second["value"])
	}
}

func TestLoadPackEntries_AnnotatesModuleMetadataFromPackMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := createTestPackFileWithMetadata(t, tmpDir, "module-pack", wapp.Metadata{
		"name":      "users",
		"namespace": "userspace.users",
		"version":   "0.1.3",
	}, []wapp.Entry{
		{
			ID:   wapp.NewID("userspace.user", "login.endpoint"),
			Kind: "http.endpoint",
			Meta: wapp.Metadata{
				"router": "public_router",
			},
			Data: map[string]any{"func": "login"},
		},
	})

	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	packEntries, err := loadPackEntries([]string{packPath}, embedReg)
	if err != nil {
		t.Fatalf("loadPackEntries failed: %v", err)
	}
	if len(packEntries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(packEntries))
	}

	meta := packEntries[0].Meta
	if got := meta.GetString("module", ""); got != "userspace/users" {
		t.Fatalf("module = %q, want userspace/users", got)
	}
	if got := meta.GetString("module_version", ""); got != "0.1.3" {
		t.Fatalf("module_version = %q, want 0.1.3", got)
	}
}

func TestLoadPackEntries_DoesNotOverrideExistingModuleMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := createTestPackFileWithMetadata(t, tmpDir, "module-pack-existing-meta", wapp.Metadata{
		"name":      "users",
		"namespace": "userspace.users",
		"version":   "0.1.3",
	}, []wapp.Entry{
		{
			ID:   wapp.NewID("userspace.user", "login.endpoint"),
			Kind: "http.endpoint",
			Meta: wapp.Metadata{
				"module":         "custom/module",
				"module_version": "9.9.9",
			},
			Data: map[string]any{"func": "login"},
		},
	})

	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	packEntries, err := loadPackEntries([]string{packPath}, embedReg)
	if err != nil {
		t.Fatalf("loadPackEntries failed: %v", err)
	}
	if len(packEntries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(packEntries))
	}

	meta := packEntries[0].Meta
	if got := meta.GetString("module", ""); got != "custom/module" {
		t.Fatalf("module = %q, want custom/module", got)
	}
	if got := meta.GetString("module_version", ""); got != "9.9.9" {
		t.Fatalf("module_version = %q, want 9.9.9", got)
	}
}

func TestLoadBootConfig_OverridesAppliedToPipelineEntries(t *testing.T) {
	t.Run("overrides from wippy.yaml are applied to entries", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, ".wippy.yaml")
		cfgContent := `version: "1.0"
override:
  "app.env:admin_email:default": "admin@example.com"
  "app:gateway:addr": ":9090"
`
		if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		prevConfigFile := configFile
		configFile = cfgPath
		t.Cleanup(func() {
			configFile = prevConfigFile
		})

		cfg, err := loadBootConfig()
		if err != nil {
			t.Fatalf("loadBootConfig: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}

		// Verify override section is loaded
		sub := cfg.Sub("override")
		keys := sub.Keys()
		if len(keys) != 2 {
			t.Fatalf("expected 2 override keys, got %d", len(keys))
		}

		// Simulate what performPack does: attach config to context and run pipeline
		ctx, _, _, embedReg, err := bootstrapPackRuntimeWithDefaults(nil, zap.NewNop(), nil)
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = embedReg.Close() })

		boot.WithConfig(ctx, cfg)

		testEntries := []regapi.Entry{
			{
				ID:   regapi.NewID("app.env", "admin_email"),
				Kind: "env.variable",
				Data: payload.New(map[string]any{
					"default":  "",
					"variable": "ADMIN_EMAIL",
				}),
			},
			{
				ID:   regapi.NewID("app", "gateway"),
				Kind: "http.server",
				Data: payload.New(map[string]any{
					"addr": ":8080",
				}),
			},
		}

		pipeline := build.New(stages.Override())
		if err := pipeline.Execute(ctx, &testEntries); err != nil {
			t.Fatalf("pipeline.Execute: %v", err)
		}

		emailData := testEntries[0].Data.Data().(map[string]any)
		if emailData["default"] != "admin@example.com" {
			t.Fatalf("expected default=admin@example.com, got %v", emailData["default"])
		}

		gwData := testEntries[1].Data.Data().(map[string]any)
		if gwData["addr"] != ":9090" {
			t.Fatalf("expected addr=:9090, got %v", gwData["addr"])
		}
	})

	t.Run("no wippy.yaml does not break pipeline", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, ".wippy.yaml.nonexistent")

		prevConfigFile := configFile
		configFile = cfgPath
		t.Cleanup(func() {
			configFile = prevConfigFile
		})

		cfg, err := loadBootConfig()
		if err != nil {
			t.Fatalf("loadBootConfig: %v", err)
		}

		// Config may be non-nil (defaults), but override section should be empty
		testEntries := []regapi.Entry{
			{
				ID:   regapi.NewID("app", "gateway"),
				Kind: "http.server",
				Data: payload.New(map[string]any{
					"addr": ":8080",
				}),
			},
		}

		ctx, _, _, embedReg, err := bootstrapPackRuntimeWithDefaults(nil, zap.NewNop(), nil)
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = embedReg.Close() })

		if cfg != nil {
			boot.WithConfig(ctx, cfg)
		}

		pipeline := build.New(stages.Override())
		if err := pipeline.Execute(ctx, &testEntries); err != nil {
			t.Fatalf("pipeline.Execute: %v", err)
		}

		gwData := testEntries[0].Data.Data().(map[string]any)
		if gwData["addr"] != ":8080" {
			t.Fatalf("expected addr unchanged at :8080, got %v", gwData["addr"])
		}
	})
}

func TestVerifyPackedResourcesSmallFileAfterChunkedFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "assets/ts.worker.js", deterministicBytes(int(wapp.ChunkSize)+257))
	writeTestFile(t, root, "assets/utils.js", []byte("export const ok = true;\n"))
	writeTestFile(t, root, "app.js", []byte("import './assets/utils.js';\n"))

	packPath := packTestResource(t, root)

	err := verifyPackedResources(packPath, []wapp.ResourceSpec{{
		ID: wapp.NewID("test", "static"),
		FS: os.DirFS(root),
	}})
	if err != nil {
		t.Fatalf("verifyPackedResources failed: %v", err)
	}
}

func TestVerifyPackedResourcesMultipleLargeChunkedFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "assets/000_small.txt", []byte("hello world"))
	writeTestFile(t, root, "assets/bundle_a.js", deterministicBytes(18*int(wapp.ChunkSize)+4567))
	writeTestFile(t, root, "assets/bundle_b.js", deterministicBytes(3*int(wapp.ChunkSize)+777))
	writeTestFile(t, root, "assets/zzz_small.txt", []byte("another small file"))

	packPath := packTestResource(t, root)

	err := verifyPackedResources(packPath, []wapp.ResourceSpec{{
		ID: wapp.NewID("test", "static"),
		FS: os.DirFS(root),
	}})
	if err != nil {
		t.Fatalf("verifyPackedResources failed: %v", err)
	}
}

func TestVerifyPackedResourcesDetectsContentMismatch(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "assets/app.js", []byte("before\n"))

	packPath := packTestResource(t, root)
	writeTestFile(t, root, "assets/app.js", []byte("after\n"))

	err := verifyPackedResources(packPath, []wapp.ResourceSpec{{
		ID: wapp.NewID("test", "static"),
		FS: os.DirFS(root),
	}})
	if err == nil {
		t.Fatal("verifyPackedResources succeeded after source changed")
	}
	if !strings.Contains(err.Error(), "content mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func createTestPackFile(t *testing.T, dir, name string, entries []wapp.Entry) string {
	t.Helper()

	return createTestPackFileWithMetadata(t, dir, name, wapp.Metadata{"name": name}, entries)
}

func createTestPackFileWithMetadata(t *testing.T, dir, name string, metadata wapp.Metadata, entries []wapp.Entry) string {
	t.Helper()

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	if err := writer.PackEntries(metadata, entries, &buf); err != nil {
		t.Fatalf("pack entries: %v", err)
	}

	path := filepath.Join(dir, name+".wapp")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write pack file: %v", err)
	}

	return path
}

func packTestResource(t *testing.T, root string) string {
	t.Helper()

	packPath := filepath.Join(t.TempDir(), "test.wapp")
	file, err := os.Create(packPath)
	if err != nil {
		t.Fatalf("create pack: %v", err)
	}

	writer := wapp.NewWriter()
	err = writer.PackWithResources(wapp.Metadata{"name": "test"}, nil, []wapp.ResourceSpec{{
		ID: wapp.NewID("test", "static"),
		FS: os.DirFS(root),
	}}, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("pack resource: %v", err)
	}

	return packPath
}

func writeTestFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func deterministicBytes(n int) []byte {
	out := make([]byte, n)
	var x uint32 = 0x12345678
	for i := range out {
		x = 1664525*x + 1013904223
		out[i] = byte(x >> 24)
	}
	return out
}

type failingPackRegistry struct {
	err error
}

func (f failingPackRegistry) Register(_ string, _ *wapp.Reader, _ *os.File) error {
	return f.err
}
