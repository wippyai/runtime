// SPDX-License-Identifier: MPL-2.0

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/internal/toolchain"
)

func TestRecoverBootsTheShippedPacksWithItsOwnHistory(t *testing.T) {
	state := t.TempDir()
	record := captureExecution(t)
	executable := runnableExecutable(t)
	deployment := filepath.Join(deploymentsPath(state), executable.Bundle.ID())
	writeCurrent(t, state, current{Directory: deploymentsDir + "/update-7", Base: executable.Bundle.ID()})

	reported := captureStderr(t, func() {
		require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "recover"}))
	})

	require.Equal(t, []string{"run", "--silent", "--", "desktop"}, record.options.Args)
	require.Equal(t, filepath.Join(deployment, "wippy.lock"), record.options.LockFile)
	history := record.options.Overrides.GetString("registry.history_path", "")
	require.Equal(t, recoveryPath(state), filepath.Dir(filepath.Dir(history)))
	require.Equal(t, historyFilename, filepath.Base(history))
	require.Equal(t, "recover: deployment "+deployment+" bundle "+executable.Bundle.ID()+
		" history "+history+"\n", reported)

	data, err := os.ReadFile(receiptPath(state))
	require.NoError(t, err)
	var written receipt
	require.NoError(t, json.Unmarshal(data, &written))
	require.Equal(t, executable.Bundle.ID(), written.Bundle)
	require.Equal(t, deployment, written.Deployment)
	require.Equal(t, history, written.History)
	digest, err := toolchain.ExecutableSHA256()
	require.NoError(t, err)
	require.Equal(t, digest, written.Executable)
	_, err = time.Parse(time.RFC3339, written.Time)
	require.NoError(t, err)

	kept, found, err := readCurrent(state)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, deploymentsDir+"/update-7", kept.Directory, "recovery leaves the current deployment selected")
}

func TestEachRecoveryGetsFreshHistoryAndKeepsThePreviousOne(t *testing.T) {
	state := t.TempDir()
	record := captureExecution(t)
	executable := runnableExecutable(t)
	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "recover"}))
	first := record.options.Overrides.GetString("registry.history_path", "")
	require.NoError(t, os.WriteFile(first, []byte("prior recovery"), 0o600))

	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "recover"}))
	second := record.options.Overrides.GetString("registry.history_path", "")
	require.NotEqual(t, first, second)
	require.NoFileExists(t, second)
	content, err := os.ReadFile(first)
	require.NoError(t, err)
	require.Equal(t, "prior recovery", string(content))
	data, err := os.ReadFile(receiptPath(state))
	require.NoError(t, err)
	var latest receipt
	require.NoError(t, json.Unmarshal(data, &latest))
	require.Equal(t, second, latest.History)
}
