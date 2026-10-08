// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"go.uber.org/zap"
)

func TestLoadReplacementEntries_UsesModuleEntrySourceRoot(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sourceRoot string
	}{
		{name: "src layout", sourceRoot: "src"},
		{name: "flat layout", sourceRoot: "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, data := range map[string]string{
				"wippy.yaml": "organization: example\nmodule: app\nexclude:\n  - " + filepath.ToSlash(filepath.Join(tc.sourceRoot, "excluded")) + "/**\n  - templates/**\n",
				filepath.Join(tc.sourceRoot, "_index.json"):             `{"namespace":"app","entries":[{"name":"live","kind":"registry.entry","value":"live"}]}`,
				filepath.Join(tc.sourceRoot, "excluded", "_index.json"): `{"namespace":"app","entries":[{"name":"excluded","kind":"ns.dependency","component":"acme/excluded","version":"*"}]}`,
				"templates/test/_index.json":                            `{"namespace":"template","entries":[{"name":"stray","kind":"ns.dependency","component":"acme/starter","version":"*"}]}`,
			} {
				path := filepath.Join(root, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(data), 0600))
			}
			if tc.sourceRoot == "src" {
				// This sibling is not excluded by the manifest: the source-root
				// boundary itself must keep it out of dependency discovery.
				require.NoError(t, os.WriteFile(filepath.Join(root, "_index.json"), []byte(`{"namespace":"outside","entries":[{"name":"stray","kind":"ns.dependency","component":"acme/starter","version":"*"}]}`), 0600))
			}

			ctx := newTestContext()
			entries, err := loadReplacementEntries(ctx, root, zap.NewNop(), payload.GetTranscoder(ctx))
			require.NoError(t, err)
			require.Len(t, entries, 1, "only the selected source tree, with module-relative exclusions, may be loaded")
			assert.Equal(t, "app:live", entries[0].ID.String())
		})
	}
}
