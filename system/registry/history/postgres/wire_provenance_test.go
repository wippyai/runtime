// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"bytes"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

// legacyEncodedOperation is the operation wire shape before provenance fields
// existed, used to produce byte-exact legacy rows.
type legacyEncodedOperation struct {
	OriginalEntry *encodedEntry
	Kind          string
	Entry         encodedEntry
}

// TestWire_LegacyRowDecodes pins that a row written by the pre-provenance
// encoder decodes under the current decoder with nil provenance fields.
func TestWire_LegacyRowDecodes(t *testing.T) {
	h := &History{handle: newMsgpackHandle()}

	legacy := []legacyEncodedOperation{{
		Kind: registry.EntryUpdate,
		Entry: encodedEntry{
			ID:             registry.NewID("test", "cache"),
			Kind:           registry.NamespaceDependency,
			DependencyRoot: true,
		},
	}}
	var buf bytes.Buffer
	require.NoError(t, codec.NewEncoder(&buf, h.handle).Encode(legacy))

	cs, err := h.decodeChangeSet(buf.Bytes())
	require.NoError(t, err)
	require.Len(t, cs, 1)
	require.Equal(t, registry.EntryUpdate, cs[0].Kind)
	require.True(t, cs[0].Entry.DependencyRoot, "PostgreSQL must decode the legacy root statement")
	require.Nil(t, cs[0].Provenance)
	require.Nil(t, cs[0].OriginalProvenance)
}

// TestWire_EmittedKeys pins the map keys the encoder emits: the v5 prov/oprov
// schema with empty halves omitted, the dead root flag never written, and a
// legacy decoder tolerating a new row.
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
	require.Contains(t, raw[0], "prov")
	require.NotContains(t, raw[0], "oprov", "an absent pair half is omitted")
	entry, ok := raw[0]["Entry"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, entry, "DependencyRoot", "new rows never write the dead root flag")
	prov, ok := raw[0]["prov"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{"module": "org/mod"}, prov)

	var legacy []legacyEncodedOperation
	require.NoError(t, codec.NewDecoder(bytes.NewReader(buf.Bytes()), handle).Decode(&legacy))
	require.Len(t, legacy, 1)
	require.Equal(t, registry.EntryCreate, legacy[0].Kind)
}
