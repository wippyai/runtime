// SPDX-License-Identifier: MPL-2.0

package packentries

import (
	"bytes"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/wapp"
)

func packReader(t *testing.T, entries []wapp.Entry, resources map[string]fstest.MapFS) *wapp.Reader {
	t.Helper()
	specs := make([]wapp.ResourceSpec, 0, len(resources))
	for name, fsys := range resources {
		specs = append(specs, wapp.ResourceSpec{ID: wapp.NewID("acme.ui", name), FS: fsys})
	}
	var buf bytes.Buffer
	require.NoError(t, wapp.NewWriter().PackWithResources(wapp.Metadata{}, entries, specs, &buf))
	reader, err := wapp.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	return reader
}

func embedEntry(data any) wapp.Entry {
	return wapp.Entry{ID: wapp.NewID("acme.ui", "assets"), Kind: embedapi.Kind, Data: data}
}

func TestDecode_DerivesMissingEmbedDigestLikeThePacker(t *testing.T) {
	assets := fstest.MapFS{"component.wasm": {Data: []byte("release 1")}, "nested/app.js": {Data: []byte("js")}}
	want, err := embedapi.ContentDigest(assets)
	require.NoError(t, err)

	for name, data := range map[string]any{"nil data": nil, "empty map": map[string]any{}} {
		t.Run(name, func(t *testing.T) {
			entries, err := Decode(packReader(t, []wapp.Entry{embedEntry(data)}, map[string]fstest.MapFS{"assets": assets}))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, map[string]any{"digest": want}, entries[0].Data.Data())
		})
	}
}

func TestDecode_KeepsStampedEmbedDigest(t *testing.T) {
	assets := fstest.MapFS{"component.wasm": {Data: []byte("release 1")}}
	entries, err := Decode(packReader(t, []wapp.Entry{embedEntry(map[string]any{"digest": "sha256-content-v1:stamped"})},
		map[string]fstest.MapFS{"assets": assets}))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"digest": "sha256-content-v1:stamped"}, entries[0].Data.Data())
}

func TestDecode_EmbedEntryWithoutPackResourceFails(t *testing.T) {
	_, err := Decode(packReader(t, []wapp.Entry{embedEntry(map[string]any{})}, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme.ui:assets")
}

func TestDecode_LeavesOtherKindsUntouched(t *testing.T) {
	entries, err := Decode(packReader(t, []wapp.Entry{{ID: wapp.NewID("acme.ui", "fn"), Kind: "function.lua", Data: map[string]any{"source": "x"}}}, nil))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"source": "x"}, entries[0].Data.Data())
}
