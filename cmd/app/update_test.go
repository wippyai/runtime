// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// updateState seeds a state whose current deployment is the shipped bundle.
func updateState(t *testing.T) (string, Executable, string) {
	t.Helper()
	state := t.TempDir()
	executable := runnableExecutable(t)
	deployment := filepath.Join(deploymentsPath(state), executable.Bundle.ID())
	_, err := executable.Bundle.Seed(deployment)
	require.NoError(t, err)
	return state, executable, deployment
}

func TestUpdateSelectsTheVerifiedCandidate(t *testing.T) {
	state, executable, deployment := updateState(t)
	var commands [][]string
	var directories []string
	run := func(_ context.Context, dir string, args []string) error {
		commands = append(commands, args)
		directories = append(directories, dir)
		return nil
	}

	launch := Launch{State: state, Op: OpUpdate, Args: []string{"acme/app"}}
	require.NoError(t, updateDeployment(t.Context(), executable, launch, deployment, run))

	require.Len(t, commands, 2)
	require.Equal(t, "update", commands[0][len(commands[0])-2])
	require.Equal(t, "acme/app", commands[0][len(commands[0])-1])
	require.Equal(t, "lint", commands[1][len(commands[1])-1])
	for index, args := range commands {
		require.Equal(t, "--state", args[0])
		require.Equal(t, OpWippy.String(), args[2])
		require.Equal(t, directories[index], filepath.Join(args[1], deploymentsDir, executable.Bundle.ID()))
	}

	record, found, err := readCurrent(state)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, deploymentsDir+"/update-1", record.Directory)
	require.Equal(t, executable.Bundle.ID(), record.Base)
	require.Equal(t, deploymentsDir+"/"+executable.Bundle.ID(), record.Previous)

	selected, err := selectDeployment(executable, Launch{State: state, Op: OpRun})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(deploymentsPath(state), "update-1"), selected)
	_, err = executable.Bundle.existing(filepath.Join(selected, lock.DefaultFilename))
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(deployment, lock.DefaultFilename), "the previous deployment stays in place")

	entries, err := os.ReadDir(deploymentsPath(state))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	expected := []string{"update-1", executable.Bundle.ID()}
	slices.Sort(names)
	slices.Sort(expected)
	require.Equal(t, expected, names)
}

func TestUpdateNumbersCandidatesInOrder(t *testing.T) {
	state, executable, deployment := updateState(t)
	run := func(context.Context, string, []string) error { return nil }
	launch := Launch{State: state, Op: OpUpdate}

	require.NoError(t, updateDeployment(t.Context(), executable, launch, deployment, run))
	require.NoError(t, updateDeployment(t.Context(), executable, launch, filepath.Join(deploymentsPath(state), "update-1"), run))

	record, found, err := readCurrent(state)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, deploymentsDir+"/update-2", record.Directory)
	require.Equal(t, deploymentsDir+"/update-1", record.Previous)
}

func TestFailedUpdateKeepsTheActiveDeployment(t *testing.T) {
	for _, failing := range []string{"update", "lint", "verify"} {
		t.Run(failing, func(t *testing.T) {
			state, executable, deployment := updateState(t)
			before, err := os.ReadFile(filepath.Join(deployment, lock.DefaultFilename))
			require.NoError(t, err)
			run := func(_ context.Context, dir string, args []string) error {
				stage := args[len(args)-1]
				if failing == "verify" && stage == "lint" {
					// A candidate whose pinned artifact is absent must not be selected.
					locked, err := lock.New(filepath.Join(dir, lock.DefaultFilename))
					require.NoError(t, err)
					for _, module := range locked.GetModuleLoadPaths() {
						if module.Module != "" {
							require.NoError(t, os.Remove(module.Path))
						}
					}
					return nil
				}
				if stage == failing || (failing == "update" && slices.Contains(args, "update")) {
					return errors.New("injected failure")
				}
				return nil
			}

			launch := Launch{State: state, Op: OpUpdate}
			require.ErrorContains(t, updateDeployment(t.Context(), executable, launch, deployment, run),
				"the active deployment is unchanged")

			_, found, err := readCurrent(state)
			require.NoError(t, err)
			require.False(t, found, "a failed update selected a deployment")
			after, err := os.ReadFile(filepath.Join(deployment, lock.DefaultFilename))
			require.NoError(t, err)
			require.Equal(t, before, after)
			entries, err := os.ReadDir(deploymentsPath(state))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.Equal(t, executable.Bundle.ID(), entries[0].Name())
		})
	}
}

func TestUpdateRunsThroughTheOperationDispatch(t *testing.T) {
	state, executable, _ := updateState(t)
	record := captureExecution(t)
	var commands [][]string
	previous := childRunner
	childRunner = func(_ context.Context, _ string, args []string) error {
		commands = append(commands, args)
		return nil
	}
	t.Cleanup(func() { childRunner = previous })

	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "update", "acme/app"}))
	require.Zero(t, record.calls, "update selects a deployment instead of starting one")
	require.Len(t, commands, 2)

	current, found, err := readCurrent(state)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, deploymentsDir+"/update-1", current.Directory)
}

func TestInterruptedUpdateLeavesNoStagingBehind(t *testing.T) {
	state, executable, deployment := updateState(t)
	// An interrupted update leaves its scratch state in place.
	leftover := filepath.Join(stagingPath(state), "update-1", deploymentsDir, executable.Bundle.ID())
	require.NoError(t, os.MkdirAll(leftover, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(leftover, lock.DefaultFilename), []byte("not: [yaml"), 0o600))

	run := func(context.Context, string, []string) error { return nil }
	require.NoError(t, updateDeployment(t.Context(), executable, Launch{State: state, Op: OpUpdate}, deployment, run))

	record, found, err := readCurrent(state)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, deploymentsDir+"/update-1", record.Directory)
	require.NoDirExists(t, stagingPath(state))
}

func TestFailedUpdateLeavesNoStagingBehind(t *testing.T) {
	state, executable, deployment := updateState(t)
	run := func(context.Context, string, []string) error { return errors.New("injected failure") }

	require.Error(t, updateDeployment(t.Context(), executable, Launch{State: state, Op: OpUpdate}, deployment, run))
	require.NoDirExists(t, stagingPath(state))
}

func TestStagingIsNotARetainedDeployment(t *testing.T) {
	state, executable, deployment := updateState(t)
	staging := filepath.Join(stagingPath(state), "update-1", deploymentsDir, executable.Bundle.ID())
	_, err := executable.Bundle.Seed(staging)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(staging, lock.DefaultFilename), []byte("not: [yaml"), 0o600))

	skipped, err := seedCache(state, deployment, executable.Bundle)
	require.NoError(t, err)
	require.Empty(t, skipped, "the artifact cache read a scratch state as a retained deployment")
}
