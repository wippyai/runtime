// SPDX-License-Identifier: MPL-2.0

//go:build unix

package exec

import (
	osexec "os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// A child killed by a signal has no exit code of its own. It is reported as
// 128+signal, with the signal itself alongside.
func TestClassifyExitReportsSignalDeath(t *testing.T) {
	if _, err := osexec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}
	cmd := osexec.CommandContext(t.Context(), "sleep", "30")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Process.Signal(syscall.SIGKILL))

	status := ClassifyExit(cmd.Wait())

	require.Equal(t, int(syscall.SIGKILL), status.Signal)
	require.Equal(t, 128+int(syscall.SIGKILL), status.Code)
	require.NoError(t, status.Err)
}
