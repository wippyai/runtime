package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
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
