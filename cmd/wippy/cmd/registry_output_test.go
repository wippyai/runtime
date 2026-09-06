// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestRegistryLoadedEntryDataIsDecoded(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("src", 0o755))
	require.NoError(t, os.WriteFile("wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join("src", "_index.yaml"), []byte(`version: "1.0"
namespace: app
entries:
  - name: sample
    kind: test.entry
    enabled: true
    source: hello
`), 0o600))
	setTestConfigFiles(t)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	entries, dtt, err := loadRegistryEntries(cmd, "wippy.lock")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	data, err := extractDataMap(&entries[0], dtt)
	require.NoError(t, err)
	require.Equal(t, "hello", data["source"])
	require.Equal(t, true, data["enabled"])

	bad := regapi.Entry{ID: regapi.NewID("app", "broken"), Data: payload.NewPayload([]byte("bad"), "unknown-format")}
	_, err = extractDataMap(&bad, dtt)
	require.ErrorContains(t, err, "app:broken")
	data, err = extractDataMap(&regapi.Entry{}, dtt)
	require.NoError(t, err)
	require.Nil(t, data)
}
