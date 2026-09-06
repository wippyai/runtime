// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestExplicitExecForwardsPositionalArguments(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("src", 0o755))
	require.NoError(t, os.WriteFile("wippy.lock", []byte("directories:\n  src: ./src\n  modules: .wippy\n"), 0o600))
	require.NoError(t, os.WriteFile("src/_index.yaml", []byte(`version: "1.0"
namespace: app
entries:
  - name: terminal
    kind: terminal.host
    lifecycle:
      auto_start: true
  - name: explicit
    kind: process.lua
    method: main
    source: |
      return {main = function(a, b, c)
        assert(a == "hello", "first argument lost")
        assert(b == "file.wapp", "pack-like argument lost")
        assert(c == "--flag", "flag argument lost")
      end}
`), 0o600))
	setTestConfigFiles(t)
	oldSilent := silentLogs
	t.Cleanup(func() { silentLogs = oldSilent })
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	require.NoError(t, cmd.ParseFlags([]string{"--exec", "app:explicit", "--", "hello", "file.wapp", "--flag"}))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd.SetContext(ctx)
	require.NoError(t, runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase))
}
