// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
)

func embedDigestOf(t *testing.T, path string) string {
	t.Helper()
	entries, err := loadEntriesFromWapp(path)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.Kind == embedapi.Kind {
			data, ok := entry.Data.Data().(map[string]any)
			require.True(t, ok)
			digest, _ := data["digest"].(string)
			return digest
		}
	}
	t.Fatal("pack has no fs.embed entry")
	return ""
}

// A pack published without an fs.embed content digest loads with the digest
// derived from its resource content, so its identity is content-derived no
// matter which runtime published it.
func TestLoadEntriesFromWapp_DerivesEmbedDigestFromPackContent(t *testing.T) {
	dir := t.TempDir()
	v1 := filepath.Join(dir, "v1.wapp")
	v1Again := filepath.Join(dir, "v1-again.wapp")
	v2 := filepath.Join(dir, "v2.wapp")
	writeEmbeddedFSWapp(t, v1, "acme.ui", "assets", map[string]string{"component.wasm": "release 1"})
	writeEmbeddedFSWapp(t, v1Again, "acme.ui", "assets", map[string]string{"component.wasm": "release 1"})
	writeEmbeddedFSWapp(t, v2, "acme.ui", "assets", map[string]string{"component.wasm": "release 2"})

	d1 := embedDigestOf(t, v1)
	assert.NotEmpty(t, d1)
	assert.Equal(t, d1, embedDigestOf(t, v1Again))
	assert.NotEqual(t, d1, embedDigestOf(t, v2))
}
