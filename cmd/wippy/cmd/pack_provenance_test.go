// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/wapp"
)

func TestPackProvenanceRejectsUnknownEntry(t *testing.T) {
	entry := wapp.Entry{ID: wapp.NewID("app", "known"), Kind: "test"}
	var packed bytes.Buffer
	err := wapp.NewWriter().PackEntries(wapp.Metadata{
		"provenance": map[string]any{
			"app:known": map[string]any{},
			"app:extra": map[string]any{},
		},
	}, []wapp.Entry{entry}, &packed)
	require.NoError(t, err)

	reader, err := entries.NewPackReader(bytes.NewReader(packed.Bytes()), nil)
	require.NoError(t, err)
	loaded, err := reader.GetEntries()
	require.NoError(t, err)

	_, err = packProvenanceFromMetadata(reader.Reader(), loaded)
	require.ErrorContains(t, err, "unknown entry app:extra")
}
