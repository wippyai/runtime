// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/lock"
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

	setTestConfigFiles(t, cfgPath)

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

	err = runPackEntries(ctx, loader, zap.NewNop(), packEntries, []string{"missing"}, defaultUseCase, "")
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

func TestRunPackEntries_NoEntrypointRunsAsServer(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	setTestConfigFiles(t, cfgPath)

	ctx, loader, _, embedReg, err := bootstrapPackRuntime(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("bootstrap pack runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = embedReg.Close()
	})

	done := make(chan error, 1)
	go func() {
		done <- runPackEntries(ctx, loader, zap.NewNop(), nil, nil, defaultUseCase, "")
	}()

	select {
	case runErr := <-done:
		t.Fatalf("runPackEntries exited immediately; expected it to keep running: %v", runErr)
	case <-time.After(2 * time.Second):
	}

	supervisorapi.TriggerShutdown(ctx, 0)

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("runPackEntries returned error: %v", runErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runPackEntries did not stop after shutdown was triggered")
	}
}

func TestRunPackEntries_TestModeWithoutTestEntrypointErrors(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	setTestConfigFiles(t, cfgPath)

	ctx, loader, _, embedReg, err := bootstrapPackRuntime(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("bootstrap pack runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = embedReg.Close()
	})

	err = runPackEntries(ctx, loader, zap.NewNop(), nil, nil, "test", "")
	if err == nil {
		t.Fatal("expected an error when there is no test entrypoint")
	}
	if !strings.Contains(err.Error(), "no test entrypoint found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPackEntries_RunModeUnknownCommandErrors(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	setTestConfigFiles(t, cfgPath)

	ctx, loader, _, embedReg, err := bootstrapPackRuntime(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("bootstrap pack runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = embedReg.Close()
	})

	err = runPackEntries(ctx, loader, zap.NewNop(), nil, []string{"nope"}, defaultUseCase, "")
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(err.Error(), `command "nope" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFromPackFiles_InvalidRequirementFailsNormalizationPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	setTestConfigFiles(t, cfgPath)

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

	err := runFromPackFiles(nil, []string{packPath}, []string{"missing"}, defaultUseCase)
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

func TestSelectEntrypoint(t *testing.T) {
	run := func(name, entryID string, main bool) packCommand {
		return packCommand{name: name, entryID: entryID, useCase: defaultUseCase, main: main}
	}

	test := func(name, entryID string) packCommand {
		return packCommand{name: name, entryID: entryID, useCase: "test"}
	}

	tests := []struct {
		name        string
		commandName string
		useCase     string
		wantEntry   string
		wantErr     string
		commands    []packCommand
	}{
		{
			name:      "default use case, no commands, runs nothing",
			useCase:   defaultUseCase,
			commands:  nil,
			wantEntry: "",
		},
		{
			name:        "explicit name match",
			useCase:     defaultUseCase,
			commands:    []packCommand{run("snake", "snake:play", false), run("edit", "snake:edit", false)},
			commandName: "edit",
			wantEntry:   "snake:edit",
		},
		{
			name:        "explicit name missing",
			useCase:     defaultUseCase,
			commands:    []packCommand{run("snake", "snake:play", false)},
			commandName: "nope",
			wantErr:     `command "nope" not found; use 'wippy run list' to see available commands`,
		},
		{
			name:      "single command without main auto-runs",
			useCase:   defaultUseCase,
			commands:  []packCommand{run("snake", "snake:play", false)},
			wantEntry: "snake:play",
		},
		{
			name:      "single command with main auto-runs",
			useCase:   defaultUseCase,
			commands:  []packCommand{run("snake", "snake:play", true)},
			wantEntry: "snake:play",
		},
		{
			name:      "multiple commands one main",
			useCase:   defaultUseCase,
			commands:  []packCommand{run("snake", "snake:play", false), run("edit", "snake:edit", true)},
			wantEntry: "snake:edit",
		},
		{
			name:     "multiple commands no main errors with sorted names",
			useCase:  defaultUseCase,
			commands: []packCommand{run("snake", "snake:play", false), run("edit", "snake:edit", false)},
			wantErr:  "no entrypoint specified; run one of: edit, snake",
		},
		{
			name:     "multiple main commands error",
			useCase:  defaultUseCase,
			commands: []packCommand{run("snake", "snake:play", true), run("edit", "snake:edit", true)},
			wantErr:  "multiple commands marked as main: snake, edit",
		},
		{
			name:      "only test use case entry, default use case runs nothing",
			useCase:   defaultUseCase,
			commands:  []packCommand{test("test", "wippy.test:runner")},
			wantEntry: "",
		},
		{
			name:      "test use case excluded, single default entry auto-runs",
			useCase:   defaultUseCase,
			commands:  []packCommand{test("test", "wippy.test:runner"), run("snake", "snake:play", false)},
			wantEntry: "snake:play",
		},
		{
			name:        "explicit test name hints at its use case",
			useCase:     defaultUseCase,
			commands:    []packCommand{test("test", "wippy.test:runner"), run("snake", "snake:play", false)},
			commandName: "test",
			wantErr:     `the "test" entrypoint belongs to the "test" use case; run it with 'wippy test'`,
		},
		{
			name:        "explicit default entry selected even when test present",
			useCase:     defaultUseCase,
			commands:    []packCommand{test("test", "wippy.test:runner"), run("snake", "snake:play", false)},
			commandName: "snake",
			wantEntry:   "snake:play",
		},
		{
			name:     "test excluded then multiple default entries require explicit",
			useCase:  defaultUseCase,
			commands: []packCommand{test("test", "wippy.test:runner"), run("snake", "snake:play", false), run("edit", "snake:edit", false)},
			wantErr:  "no entrypoint specified; run one of: edit, snake",
		},
		{
			name:      "test use case selects the test entrypoint regardless of name",
			useCase:   "test",
			commands:  []packCommand{run("snake", "snake:play", true), test("runner", "wippy.test:runner")},
			wantEntry: "wippy.test:runner",
		},
		{
			name:     "test use case without a test entrypoint errors",
			useCase:  "test",
			commands: []packCommand{run("snake", "snake:play", false)},
			wantErr:  "no test entrypoint found",
		},
		{
			name:     "test use case with no commands errors",
			useCase:  "test",
			commands: nil,
			wantErr:  "no test entrypoint found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectEntrypoint(tc.commands, tc.commandName, tc.useCase)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("selectEntrypoint error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("selectEntrypoint unexpected error: %v", err)
			}

			if got != tc.wantEntry {
				t.Fatalf("selectEntrypoint = %q, want %q", got, tc.wantEntry)
			}
		})
	}
}

func TestCommandsFromEntries(t *testing.T) {
	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("app", "gateway"),
			Kind: "http.service",
			Meta: map[string]any{"comment": "not a command"},
		},
		{
			ID:   regapi.NewID("app", "cli"),
			Kind: "function.lua",
			Meta: map[string]any{"command": map[string]any{"name": "cli"}},
		},
		{
			ID:   regapi.NewID("app", "serve"),
			Kind: "process.lua",
			Meta: map[string]any{"command": map[string]any{"name": "serve", "main": true}},
		},
		{
			ID:   regapi.NewID("app", "runner"),
			Kind: "process.lua",
			Meta: map[string]any{"command": map[string]any{"name": "test", "use_case": "test"}},
		},
	}

	got := commandsFromEntries(entries)
	want := []packCommand{
		{name: "serve", entryID: "app:serve", useCase: defaultUseCase, main: true},
		{name: "test", entryID: "app:runner", useCase: "test"},
	}

	if len(got) != len(want) {
		t.Fatalf("commandsFromEntries returned %d commands, want %d: %+v", len(got), len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCollectPackCommandsFiltersDependencyModules(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "wippy.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	setTestConfigFiles(t, cfgPath)

	ctx, loader, _, embedReg, err := bootstrapPackRuntime(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("bootstrap pack runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = embedReg.Close()
	})
	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := loader.Start(appCtx); err != nil {
		t.Fatalf("start loader: %v", err)
	}
	t.Cleanup(func() {
		_ = shutdown.Perform(appCtx, loader, zap.NewNop(), true)
	})

	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("wippy.test", "runner"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{"source": "return {}"}),
			Meta: map[string]any{
				"module":  "wippy/test",
				"command": map[string]any{"name": "test"},
			},
		},
		{
			ID:   regapi.NewID("app", "serve"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{"source": "return {}"}),
			Meta: map[string]any{
				"module":  "kickside/kickside",
				"command": map[string]any{"name": "serve", "main": true},
			},
		},
	}
	if err := applyPackEntries(appCtx, entries, zap.NewNop()); err != nil {
		t.Fatalf("apply entries: %v", err)
	}

	commands, err := collectPackCommands(appCtx, "kickside/kickside")
	if err != nil {
		t.Fatalf("collect pack commands: %v", err)
	}
	if len(commands) != 1 || commands[0].entryID != "app:serve" {
		t.Fatalf("commands = %#v, want only app:serve", commands)
	}

	entryID, err := findPackCommandForModule(appCtx, "", defaultUseCase, "kickside/kickside")
	if err != nil {
		t.Fatalf("find pack command: %v", err)
	}
	if entryID != "app:serve" {
		t.Fatalf("entryID = %q, want app:serve", entryID)
	}
}

func TestPackCommandAllowed(t *testing.T) {
	tests := []struct {
		name       string
		meta       map[string]any
		mainModule string
		want       bool
	}{
		{
			name: "moduleless command allowed without pack identity",
			meta: map[string]any{"command": map[string]any{"name": "serve"}},
			want: true,
		},
		{
			name: "dependency command excluded without pack identity",
			meta: map[string]any{"module": "wippy/test", "command": map[string]any{"name": "test"}},
			want: false,
		},
		{
			name:       "main module command allowed",
			meta:       map[string]any{"module": "kickside/kickside", "command": map[string]any{"name": "serve"}},
			mainModule: "kickside/kickside",
			want:       true,
		},
		{
			name:       "dependency command excluded when main module known",
			meta:       map[string]any{"module": "wippy/test", "command": map[string]any{"name": "test"}},
			mainModule: "kickside/kickside",
			want:       false,
		},
		{
			name:       "moduleless command allowed when main module known",
			meta:       map[string]any{"command": map[string]any{"name": "serve"}},
			mainModule: "kickside/kickside",
			want:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := packCommandAllowed(tc.meta, tc.mainModule); got != tc.want {
				t.Fatalf("packCommandAllowed() = %v, want %v", got, tc.want)
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

		setTestConfigFiles(t, cfgPath)

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

		setTestConfigFiles(t, cfgPath)

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

func TestLoadPackEntries_RegistersModuleOwnedEmbeddedResources(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := createModulePackWithEmbeddedResource(t, tmpDir, "ui-pack", wapp.Metadata{
		"name":      "ui",
		"namespace": "acme.ui",
		"version":   "0.2.0",
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

	fsys, err := embedReg.GetFSForEntry(packEntries[0])
	if err != nil {
		t.Fatalf("GetFSForEntry failed: %v", err)
	}
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatalf("read embedded file: %v", err)
	}
	if string(data) != "<main>ok</main>\n" {
		t.Fatalf("embedded file content = %q", string(data))
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

func TestLoadPackEntries_MonolithicPackRegistersModuleResourceAliases(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "static")
	writeTestFile(t, root, "index.html", []byte("<html>module</html>\n"))

	packPath := createTestResourcePackFile(t, tmpDir, "snapshot", wapp.Metadata{
		"name": "snapshot",
	}, []wapp.Entry{{
		ID:   wapp.NewID("acme.ui", "static_fs"),
		Kind: "fs.embed",
		Meta: wapp.Metadata{
			"module":         "acme/ui",
			"module_version": "1.2.3",
		},
	}}, []wapp.ResourceSpec{{
		ID: wapp.NewID("acme.ui", "static_fs"),
		FS: os.DirFS(root),
	}})

	embedReg := embedpkg.NewRegistry()
	defer func() { _ = embedReg.Close() }()

	packEntries, err := loadPackEntries([]string{packPath}, embedReg)
	if err != nil {
		t.Fatalf("loadPackEntries failed: %v", err)
	}
	if len(packEntries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(packEntries))
	}

	fsys, err := embedReg.GetFSForEntry(packEntries[0])
	if err != nil {
		t.Fatalf("GetFSForEntry failed: %v", err)
	}
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatalf("read aliased resource: %v", err)
	}
	if string(data) != "<html>module</html>\n" {
		t.Fatalf("aliased resource data = %q", string(data))
	}
}

func TestCollectEmbeddedPackResourcesCarriesDependencyResources(t *testing.T) {
	tmpDir := t.TempDir()
	depRoot := filepath.Join(tmpDir, "dep")
	writeTestFile(t, depRoot, "index.html", []byte("<html>dep</html>\n"))

	depPack := createTestResourcePackFile(t, tmpDir, "dep", wapp.Metadata{
		"name":      "ui",
		"namespace": "acme.ui",
		"version":   "1.2.3",
	}, []wapp.Entry{{
		ID:   wapp.NewID("acme.ui", "static_fs"),
		Kind: "fs.embed",
	}}, []wapp.ResourceSpec{{
		ID: wapp.NewID("acme.ui", "static_fs"),
		FS: os.DirFS(depRoot),
	}})

	resources, handles, err := collectEmbeddedPackResources([]lock.ModuleLoadPath{{
		Module:  "acme/ui",
		Version: "1.2.3",
		Path:    depPack,
	}}, nil)
	defer func() { _ = closeEmbeddedPackResourceHandles(handles) }()
	if err != nil {
		t.Fatalf("collectEmbeddedPackResources failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(resources))
	}
	data, err := fs.ReadFile(resources[0].FS, "index.html")
	if err != nil {
		t.Fatalf("read carried resource: %v", err)
	}
	if string(data) != "<html>dep</html>\n" {
		t.Fatalf("carried resource data = %q", string(data))
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

		setTestConfigFiles(t, cfgPath)

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

	t.Run("missing optional default does not break pipeline", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)
		setTestConfigFiles(t)

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

func createModulePackWithEmbeddedResource(t *testing.T, dir, name string, metadata wapp.Metadata) string {
	t.Helper()

	root := filepath.Join(dir, name+"-static")
	writeTestFile(t, root, "index.html", []byte("<main>ok</main>\n"))

	packPath := filepath.Join(dir, name+".wapp")
	file, err := os.Create(packPath)
	if err != nil {
		t.Fatalf("create pack file: %v", err)
	}

	writer := wapp.NewWriter()
	err = writer.PackWithResources(metadata, []wapp.Entry{
		{
			ID:   wapp.NewID("app", "app_fs"),
			Kind: "fs.embed",
			Data: map[string]any{},
		},
	}, []wapp.ResourceSpec{
		{
			ID: wapp.NewID("app", "app_fs"),
			FS: os.DirFS(root),
		},
	}, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("pack with resource: %v", err)
	}

	return packPath
}

func createTestResourcePackFile(t *testing.T, dir, name string, metadata wapp.Metadata, entries []wapp.Entry, resources []wapp.ResourceSpec) string {
	t.Helper()

	packPath := filepath.Join(dir, name+".wapp")
	file, err := os.Create(packPath)
	if err != nil {
		t.Fatalf("create pack file: %v", err)
	}

	writer := wapp.NewWriter()
	err = writer.PackWithResources(metadata, entries, resources, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("pack with resources: %v", err)
	}

	return packPath
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
