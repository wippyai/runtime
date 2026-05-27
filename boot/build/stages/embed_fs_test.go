// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"go.uber.org/zap"
)

func TestEmbedFSCollectsModuleRelativeDirectory(t *testing.T) {
	moduleRoot := t.TempDir()
	staticDir := filepath.Join(moduleRoot, "static", "app")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "app.js"), []byte("export const ok = true;\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	t.Chdir(moduleRoot)

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "static_fs"),
		Kind: dirapi.Kind,
		Meta: attrs.NewBagFrom(map[string]any{"module": "acme/ui"}),
		Data: payload.New(map[string]any{
			"base":      dirapi.BaseModule,
			"directory": "./static/app",
		}),
	}

	resources, err := collectResources([]registry.Entry{entry}, zap.NewNop())
	if err != nil {
		t.Fatalf("collectResources failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(resources))
	}
	if got := resources[0].ID.String(); got != "acme.ui:static_fs" {
		t.Fatalf("resource id = %q, want acme.ui:static_fs", got)
	}
	data, err := fs.ReadFile(resources[0].FS, "app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	if string(data) != "export const ok = true;\n" {
		t.Fatalf("embedded app.js = %q", string(data))
	}

	transformed := transformEntries([]registry.Entry{entry}, []registry.ID{entry.ID})
	if len(transformed) != 1 {
		t.Fatalf("transformed count = %d, want 1", len(transformed))
	}
	if transformed[0].Kind != embedapi.Kind {
		t.Fatalf("transformed kind = %q, want %q", transformed[0].Kind, embedapi.Kind)
	}
}
