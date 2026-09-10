// SPDX-License-Identifier: MPL-2.0

//go:build windows

package native

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

func TestPTYProcessStartReportsUnavailable(t *testing.T) {
	executor := NewNativeExecutor(zap.NewNop(), &exec.NativeExecutorConfig{})
	process, err := executor.NewProcess("cmd /c exit 0", exec.ProcessOptions{PTY: &exec.PTYOptions{Width: 80, Height: 24}})
	require.NoError(t, err)
	require.ErrorIs(t, process.Start(), exec.ErrPTYUnavailable)
}
