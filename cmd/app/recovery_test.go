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
	require.Equal(t, recoveryHistoryPath(state), record.options.Overrides.GetString("registry.history_path", ""))
	require.Equal(t, "recover: deployment "+deployment+" bundle "+executable.Bundle.ID()+
		" history "+recoveryHistoryPath(state)+"\n", reported)

	data, err := os.ReadFile(receiptPath(state))
	require.NoError(t, err)
	var written receipt
	require.NoError(t, json.Unmarshal(data, &written))
	require.Equal(t, executable.Bundle.ID(), written.Bundle)
	require.Equal(t, deployment, written.Deployment)
	require.Equal(t, recoveryHistoryPath(state), written.History)
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
