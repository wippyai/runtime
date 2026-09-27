// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestCurrentProcessIsNotLPAC(t *testing.T) {
	isLPAC, err := CurrentProcessIsLPAC()
	require.NoError(t, err)
	require.False(t, isLPAC)
}

func TestTokenHasLPACAccessDistinguishesAppContainers(t *testing.T) {
	for _, test := range []struct {
		name   string
		optOut bool
		want   bool
	}{
		{name: "ordinary AppContainer", optOut: false, want: false},
		{name: "less-privileged AppContainer", optOut: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sandbox, err := NewSandbox()
			require.NoError(t, err)
			workDir := t.TempDir()
			workGrant, err := sandbox.GrantPath(workDir, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|
				windows.FILE_GENERIC_EXECUTE|windows.DELETE)
			require.NoError(t, err)
			executable, err := os.Executable()
			require.NoError(t, err)
			executableGrant, err := sandbox.GrantPath(executable, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_EXECUTE)
			require.NoError(t, err)
			job, err := NewJob(0)
			require.NoError(t, err)
			stdin, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
			require.NoError(t, err)
			stdout, err := os.CreateTemp(workDir, "stdout-")
			require.NoError(t, err)
			stderr, err := os.CreateTemp(workDir, "stderr-")
			require.NoError(t, err)

			spawned, err := sandbox.spawnSuspended(SpawnRequest{
				Path: executableGrant.Path, Args: []string{executableGrant.Path, "-test.run=^$"},
				WorkDir: workGrant.Path, Stdin: windows.Handle(stdin.Fd()), Stdout: windows.Handle(stdout.Fd()),
				Stderr: windows.Handle(stderr.Fd()), Job: job,
			}, test.optOut)
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = job.Kill(1)
				_ = spawned.Kill(1)
				_, _ = spawned.Wait()
				require.NoError(t, spawned.Close())
				require.NoError(t, stdin.Close())
				require.NoError(t, stdout.Close())
				require.NoError(t, stderr.Close())
				require.NoError(t, job.Close())
				require.NoError(t, executableGrant.Close())
				require.NoError(t, workGrant.Close())
				require.NoError(t, sandbox.Close())
			})

			var token windows.Token
			require.NoError(t, windows.OpenProcessToken(spawned.Process,
				windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &token))
			defer token.Close()
			isContainer, err := tokenBool(token, 29)
			require.NoError(t, err)
			require.True(t, isContainer)
			isLPAC, err := tokenHasLPACAccess(token)
			require.NoError(t, err)
			require.Equal(t, test.want, isLPAC)
			verifyErr := sandbox.VerifySpawned(spawned, job)
			if test.want {
				require.NoError(t, verifyErr)
			} else {
				require.ErrorContains(t, verifyErr, "less-privileged AppContainer")
			}
		})
	}
}
