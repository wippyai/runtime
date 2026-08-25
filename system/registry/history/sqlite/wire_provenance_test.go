// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

// legacyEncodedOperation is the operation wire shape before provenance fields
// existed, used to produce byte-exact legacy rows.
type legacyEncodedOperation struct {
	OriginalEntry *encodedEntry
	Kind          string
	Entry         encodedEntry
}

func provenancedOp(id registry.ID) registry.Operation {
	return registry.Operation{
		Kind:  registry.EntryCreate,
		Entry: registry.Entry{ID: id, Kind: "store.memory"},
		Provenance: &registry.EntryProvenance{
			Module:  "org/mod",
			Version: "1.2.3",
			Digest:  "sha256:abc",
			Root:    true,
		},
	}
}

// TestWire_LegacyRowDecodes pins that a row written by the pre-provenance
// encoder decodes under the current decoder with nil provenance fields.
func TestWire_LegacyRowDecodes(t *testing.T) {
	h := &History{handle: newMsgpackHandle()}

	legacy := []legacyEncodedOperation{{
		Kind:  registry.EntryUpdate,
		Entry: encodedEntry{ID: registry.NewID("test", "cache"), Kind: "store.memory"},
	}}
	var buf bytes.Buffer
	require.NoError(t, codec.NewEncoder(&buf, h.handle).Encode(legacy))

	cs, err := h.decodeChangeSet(buf.Bytes())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Equal(t, registry.EntryUpdate, cs[0].Kind)
	require.Nil(t, cs[0].Provenance)
	require.Nil(t, cs[0].OriginalProvenance)
}

// TestWire_EmptyLegacyRowDecodes pins the empty-array row the schema seeds for
// the root version.
func TestWire_EmptyLegacyRowDecodes(t *testing.T) {
	h := &History{handle: newMsgpackHandle()}

	cs, err := h.decodeChangeSet([]byte{0x90})
	require.NoError(t, err)
	require.Empty(t, cs)
}

// TestWire_ProvenanceRoundTrip pins the full Save/Get path: provenance fields
// survive the database round trip and land on the decoded operations.
func TestWire_ProvenanceRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wire.db")
	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	root, err := hist.Head()
	require.NoError(t, err)

	op := provenancedOp(registry.NewID("test", "cache"))
	v1 := version.FromParent(root, root.ID()+1)
	require.NoError(t, hist.Save(v1, registry.ChangeSet{op}, true))

	decoded, err := hist.Get(v1)
	require.NoError(t, err)
	require.Len(t, decoded, 1)
	require.NotNil(t, decoded[0].Provenance)
	require.Equal(t, *op.Provenance, *decoded[0].Provenance)
	require.Nil(t, decoded[0].OriginalProvenance)
}

// TestWire_EmittedKeys pins the map keys the encoder emits, so the wire shape
// cannot drift silently: a legacy decoder ignores the two new keys, and the
// current decoder finds them under exactly these names.
func TestWire_EmittedKeys(t *testing.T) {
	handle := newMsgpackHandle()
	ops := []encodedOperation{{
		Kind:       registry.EntryCreate,
		Entry:      encodedEntry{ID: registry.NewID("test", "cache"), Kind: "store.memory"},
		Provenance: &registry.EntryProvenance{Module: "org/mod"},
	}}
	var buf bytes.Buffer
	require.NoError(t, codec.NewEncoder(&buf, handle).Encode(ops))

	var raw []map[string]any
	require.NoError(t, codec.NewDecoder(bytes.NewReader(buf.Bytes()), handle).Decode(&raw))
	require.Len(t, raw, 1)
	require.Contains(t, raw[0], "Provenance")
	require.Contains(t, raw[0], "OriginalProvenance")
	require.Nil(t, raw[0]["OriginalProvenance"], "an absent pair half is emitted nil, matching OriginalEntry")
	require.Contains(t, raw[0], "Entry")
	require.Contains(t, raw[0], "Kind")
	prov, ok := raw[0]["Provenance"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{"module": "org/mod"}, prov, "provenance serializes under its json tags with empty fields omitted")

	// A legacy decoder is the old struct shape: it must decode a new row
	// without error, dropping the provenance it does not know.
	var legacy []legacyEncodedOperation
	require.NoError(t, codec.NewDecoder(bytes.NewReader(buf.Bytes()), handle).Decode(&legacy))
	require.Len(t, legacy, 1)
	require.Equal(t, registry.EntryCreate, legacy[0].Kind)
}
