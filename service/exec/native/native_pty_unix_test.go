// SPDX-License-Identifier: MPL-2.0

//go:build unix

package native

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

func TestPTYProcessRefusesCloseStdin(t *testing.T) {
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess("cat", exec.ProcessOptions{PTY: &exec.PTYOptions{Width: 80, Height: 24}})
	require.NoError(t, err)
	require.NoError(t, process.Start())
	closer, ok := process.(exec.StdinCloser)
	require.True(t, ok)
	require.ErrorIs(t, closer.CloseStdin(), ErrStdinPTY)
	require.NoError(t, process.Signal(int(syscall.SIGKILL)))
	_ = process.Wait()
}
