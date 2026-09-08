// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCLIProgressReporterWritesPlainProgress(t *testing.T) {
	var output bytes.Buffer
	reporter := newProgressReporter(context.Background(), &output, true)
	reporter.lintTotal = 2

	reporter.Send(lintProgressMsg{
		entry:   "app:main",
		percent: 0.5,
		checked: 1,
	})
	reporter.Close()

	require.Contains(t, output.String(), "Linting... 1/2 entries ( 50%) app:main")
	require.NotContains(t, output.String(), "\x1b[")
}

func TestCLIProgressReporterSuppressesNonTTYOutput(t *testing.T) {
	var output bytes.Buffer
	reporter := newProgressReporter(context.Background(), &output, false)

	reporter.Send(progressMsg{percent: 0.5, status: "working"})
	reporter.Close()

	require.Empty(t, output.String())
}

func TestCLIProgressReporterObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	reporter := newProgressReporter(ctx, &output, true)
	cancel()

	reporter.Send(lintProgressMsg{})
	require.ErrorIs(t, reporter.Err(), context.Canceled)
	require.Empty(t, output.String())
}

func TestCLIProgressReporterUsesNoTTYForRegularFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress-output-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	require.False(t, isTerminalFile(file))
	reporter := newCLIProgressReporter(context.Background(), file)
	reporter.Send(lintProgressMsg{percent: 1, checked: 1})
	reporter.Close()

	info, err := file.Stat()
	require.NoError(t, err)
	require.Zero(t, info.Size())
}
