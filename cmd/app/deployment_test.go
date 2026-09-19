// SPDX-License-Identifier: MPL-2.0

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureStderr collects what the runner reports while fn runs.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	previous := os.Stderr
	os.Stderr = writer
	done := make(chan []byte, 1)
	go func() {
		var collected []byte
		buffer := make([]byte, 4096)
		for {
			read, err := reader.Read(buffer)
			collected = append(collected, buffer[:read]...)
			if err != nil {
				break
			}
		}
		done <- collected
	}()
	fn()
	os.Stderr = previous
	require.NoError(t, writer.Close())
	collected := <-done
	require.NoError(t, reader.Close())
	return string(collected)
}

func writeCurrent(t *testing.T, state string, record current) {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(currentPath(state), data, 0o600))
}

func TestDeploymentSelectionFallsBackToTheShippedBundle(t *testing.T) {
	state := t.TempDir()
	executable := runnableExecutable(t)
	launch := Launch{State: state, Op: OpRun}

	selected, err := selectDeployment(executable, launch)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(deploymentsPath(state), executable.Bundle.ID()), selected)
}

func TestDeploymentSelectionFollowsACurrentOfThisExecutable(t *testing.T) {
	state := t.TempDir()
	executable := runnableExecutable(t)
	writeCurrent(t, state, current{Directory: deploymentsDir + "/update-3", Base: executable.Bundle.ID()})

	selected, err := selectDeployment(executable, Launch{State: state, Op: OpRun})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(deploymentsPath(state), "update-3"), selected)
}

func TestDeploymentSelectionKeepsAForeignCurrentAsHistory(t *testing.T) {
	state := t.TempDir()
	executable := runnableExecutable(t)
	writeCurrent(t, state, current{Directory: deploymentsDir + "/update-3", Base: "another-executable"})

	var selected string
	var err error
	reported := captureStderr(t, func() { selected, err = selectDeployment(executable, Launch{State: state, Op: OpRun}) })
	require.NoError(t, err)
	require.Equal(t, filepath.Join(deploymentsPath(state), executable.Bundle.ID()), selected)
	require.Contains(t, reported, "update-3")
	require.Contains(t, reported, "superseded")

	record, found, err := readCurrent(state)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "another-executable", record.Base, "the foreign record stays in place as history")
}

func TestDeploymentSelectionRejectsAnUnreadableCurrent(t *testing.T) {
	for _, content := range []string{"{", `{"directory":"../outside","base":"x"}`, `{"directory":"elsewhere/one","base":"x"}`} {
		state := t.TempDir()
		require.NoError(t, os.WriteFile(currentPath(state), []byte(content), 0o600))

		_, err := selectDeployment(runnableExecutable(t), Launch{State: state, Op: OpRun})
		require.ErrorContains(t, err, "current deployment record is not usable")
	}
}

func TestRecoveryIgnoresCurrentAndSelectsItsOwnHistory(t *testing.T) {
	state := t.TempDir()
	executable := runnableExecutable(t)
	writeCurrent(t, state, current{Directory: deploymentsDir + "/update-3", Base: executable.Bundle.ID()})
	launch := Launch{State: state, Op: OpRecover}

	selected, err := selectDeployment(executable, launch)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(deploymentsPath(state), executable.Bundle.ID()), selected)
	require.Equal(t, recoveryHistoryPath(state), historyFor(launch))
	require.Equal(t, historyPath(state), historyFor(Launch{State: state, Op: OpRun}))
}
