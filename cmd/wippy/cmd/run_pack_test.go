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
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestRunPackEntries_LoadStatePathSkipsDependencyExpansion(t *testing.T) {
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

	// This malformed ns.dependency entry fails if dependency expansion runs.
	// For pack execution we expect baseline LoadState behavior.
	packEntries := []regapi.Entry{
		{
			ID:   regapi.NewID("app", "deps"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "",
			}),
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
		t.Fatal("expected command lookup error")
	}

	errText := err.Error()
	if !strings.Contains(errText, `command "missing" not found in pack`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(errText, "failed to expand changeset") {
		t.Fatalf("unexpected dependency expansion error: %v", err)
	}
}

func TestRunFromPackFiles_SnapshotLoadPath(t *testing.T) {
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
	if !strings.Contains(errText, `command "missing" not found in pack`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(errText, "failed to execute pipeline") || strings.Contains(errText, "failed to decode requirement") {
		t.Fatalf("unexpected re-link/pipeline error: %v", err)
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

func createTestPackFile(t *testing.T, dir, name string, entries []wapp.Entry) string {
	t.Helper()

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	if err := writer.PackEntries(wapp.Metadata{"name": name}, entries, &buf); err != nil {
		t.Fatalf("pack entries: %v", err)
	}

	path := filepath.Join(dir, name+".wapp")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write pack file: %v", err)
	}

	return path
}

type failingPackRegistry struct {
	err error
}

func (f failingPackRegistry) Register(_ string, _ *wapp.Reader, _ *os.File) error {
	return f.err
}
