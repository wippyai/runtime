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

func TestTestUseCaseForwardsPositionalArguments(t *testing.T) {
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
  - name: runner
    kind: process.lua
    meta:
      command:
        name: runner
        use_case: test
    method: main
    source: |
      return {main = function(a, b)
        assert(a == "storage", "filter argument lost")
        assert(b == "--verbose", "flag argument lost")
      end}
`), 0o600))
	setTestConfigFiles(t)
	oldSilent := silentLogs
	t.Cleanup(func() { silentLogs = oldSilent })
	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	require.NoError(t, cmd.ParseFlags([]string{"--", "storage", "--verbose"}))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd.SetContext(ctx)
	require.NoError(t, runWithUseCase(cmd, cmd.Flags().Args(), "test"))
}

func TestEntrypointArgs(t *testing.T) {
	tests := []struct {
		name     string
		useCase  string
		wantName string
		args     []string
		wantArgs []string
	}{
		{name: "default use case without tokens", useCase: defaultUseCase},
		{name: "default use case names its entrypoint", useCase: defaultUseCase, args: []string{"greet", "a"}, wantName: "greet", wantArgs: []string{"a"}},
		{name: "test use case forwards every token", useCase: "test", args: []string{"storage", "--verbose"}, wantArgs: []string{"storage", "--verbose"}},
		{name: "test use case without tokens", useCase: "test"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, args := entrypointArgs(tc.args, tc.useCase)
			require.Equal(t, tc.wantName, name)
			require.Equal(t, tc.wantArgs, args)
		})
	}
}
