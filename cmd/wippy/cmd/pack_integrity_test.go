// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wapp"
)

func TestB12VerifyRejectsPackedExtraFile(t *testing.T) {
	id := wapp.NewID("app", "assets")
	expected := wapp.ResourceSpec{ID: id, FS: fstest.MapFS{
		"expected.txt": &fstest.MapFile{Data: []byte("expected")},
	}}
	packed := wapp.ResourceSpec{ID: id, FS: fstest.MapFS{
		"expected.txt": &fstest.MapFile{Data: []byte("expected")},
		"extra.txt":    &fstest.MapFile{Data: []byte("must be rejected")},
	}}
	packPath := filepath.Join(t.TempDir(), "extra.wapp")
	file, err := os.Create(packPath)
	require.NoError(t, err)
	err = wapp.NewWriter().PackWithResources(wapp.Metadata{}, nil, []wapp.ResourceSpec{packed}, file)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	err = verifyPackedResources(packPath, []wapp.ResourceSpec{expected})
	require.Error(t, err)
	require.Contains(t, err.Error(), "extra.txt")
	require.Contains(t, err.Error(), "unexpected")
}
