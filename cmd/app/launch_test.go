// SPDX-License-Identifier: MPL-2.0

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func testExecutable() Executable {
	return Executable{Name: "grammar-test", Command: "desktop"}
}

func TestLaunchGrammar(t *testing.T) {
	state := t.TempDir()
	for _, testCase := range []struct {
		name     string
		args     []string
		op       Op
		expected []string
	}{
		{name: "bare", args: nil, op: OpRun, expected: []string{}},
		{name: "run", args: []string{"run", "one", "two"}, op: OpRun, expected: []string{"one", "two"}},
		{name: "implied run", args: []string{"one", "two"}, op: OpRun, expected: []string{"one", "two"}},
		{name: "update", args: []string{"update", "acme/app"}, op: OpUpdate, expected: []string{"acme/app"}},
		{name: "recover", args: []string{"recover"}, op: OpRecover, expected: []string{}},
		{name: "wippy", args: []string{"wippy", "lint", "--json"}, op: OpWippy, expected: []string{"lint", "--json"}},
		{name: "verb after run", args: []string{"run", "update", "wippy"}, op: OpRun, expected: []string{"update", "wippy"}},
		{name: "flag after verb", args: []string{"run", "--state", "elsewhere"}, op: OpRun, expected: []string{"--state", "elsewhere"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			launch, err := parseLaunch(testExecutable(), append([]string{"--state", state}, testCase.args...))
			require.NoError(t, err)
			require.Equal(t, testCase.op, launch.Op)
			require.Equal(t, testCase.expected, launch.Args)
			require.Equal(t, state, launch.State)
			require.True(t, launch.Explicit)
			require.Equal(t, "desktop", launch.Command)
		})
	}
}

func TestLaunchStateSelection(t *testing.T) {
	launch, err := parseLaunch(testExecutable(), []string{"run"})
	require.NoError(t, err)
	require.False(t, launch.Explicit)
	config, err := os.UserConfigDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(config, "grammar-test"), launch.State)

	working, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, working, launch.Dir)

	relative, err := parseLaunch(testExecutable(), []string{"--state", "relative-state", "run"})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(working, "relative-state"), relative.State)
}

func TestLaunchRejectsStateWithoutValue(t *testing.T) {
	_, err := parseLaunch(testExecutable(), []string{"--state"})
	require.Error(t, err)
}

func TestOpString(t *testing.T) {
	require.Equal(t, "run", OpRun.String())
	require.Equal(t, "update", OpUpdate.String())
	require.Equal(t, "recover", OpRecover.String())
	require.Equal(t, "wippy", OpWippy.String())
}

func TestDeclarationIsValidatedBeforeAnyFilesystemEffect(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		executable Executable
		message    string
	}{
		{name: "empty name", executable: Executable{Command: "desktop"}, message: "invalid application name"},
		{name: "upper case name", executable: Executable{Name: "Desktop", Command: "desktop"}, message: "invalid application name"},
		{name: "missing command", executable: Executable{Name: "desktop"}, message: "application command is required"},
		{
			name:       "data name",
			executable: Executable{Name: "desktop", Command: "desktop", Data: map[string]string{"lower": "data"}},
			message:    "invalid application data environment binding",
		},
		{
			name:       "data escape",
			executable: Executable{Name: "desktop", Command: "desktop", Data: map[string]string{"DATA": "../outside"}},
			message:    "invalid application data environment binding",
		},
		{
			name:       "data absolute",
			executable: Executable{Name: "desktop", Command: "desktop", Data: map[string]string{"DATA": string(filepath.Separator) + "outside"}},
			message:    "invalid application data environment binding",
		},
		{
			name:       "data nul",
			executable: Executable{Name: "desktop", Command: "desktop", Data: map[string]string{"DATA": "data\x00file"}},
			message:    "invalid application data environment binding",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "absent")
			err := Run(t.Context(), testCase.executable, []string{"--state", state, "run"})
			require.ErrorContains(t, err, testCase.message)
			require.NoDirExists(t, state)
		})
	}
}

func TestOwnedProbeReportsAnotherHolder(t *testing.T) {
	state := t.TempDir()
	owned, err := probeOwned(state)
	require.NoError(t, err)
	require.False(t, owned, "an absent lock file reports no owner")

	unlock, err := lockState(state)
	require.NoError(t, err)
	owned, err = probeOwned(state)
	require.NoError(t, err)
	require.True(t, owned)

	require.NoError(t, unlock())
	owned, err = probeOwned(state)
	require.NoError(t, err)
	require.False(t, owned)
}

func TestOwnedProbeLeavesAbsentStateAbsent(t *testing.T) {
	state := filepath.Join(t.TempDir(), "absent")
	owned, err := probeOwned(state)
	require.NoError(t, err)
	require.False(t, owned)
	require.NoDirExists(t, state)
}

func TestLaunchCarriesOwnership(t *testing.T) {
	state := t.TempDir()
	unlock, err := lockState(state)
	require.NoError(t, err)
	defer func() { _ = unlock() }()
	launch, err := parseLaunch(testExecutable(), []string{"--state", state, "run"})
	require.NoError(t, err)
	require.True(t, launch.Owned)
}
