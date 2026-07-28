// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func assertAtomicPackFailure(t *testing.T, output string, err error) {
	t.Helper()
	require.Error(t, err)
	got, readErr := os.ReadFile(output)
	require.NoError(t, readErr)
	require.Equal(t, []byte("sentinel"), got)
	residue, globErr := filepath.Glob(filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+".tmp-*"))
	require.NoError(t, globErr)
	require.Empty(t, residue)
}

func TestB06PackWriterFailurePreservesDestination(t *testing.T) {
	output := filepath.Join(t.TempDir(), "snapshot.wapp")
	require.NoError(t, os.WriteFile(output, []byte("sentinel"), 0o644))
	cause := errors.New("injected writer failure")

	_, err := writePackAtomically(output, nil, func(w io.Writer) error {
		_, writeErr := w.Write([]byte("partial pack"))
		require.NoError(t, writeErr)
		return cause
	}, func(string) error { return nil })
	require.ErrorIs(t, err, cause)
	assertAtomicPackFailure(t, output, err)

	_, err = writePackAtomically(output, nil, func(w io.Writer) error {
		_, writeErr := w.Write([]byte("published pack"))
		return writeErr
	}, func(string) error { return nil })
	require.NoError(t, err)
	info, statErr := os.Stat(output)
	require.NoError(t, statErr)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestB07PackIntegrityFailurePreservesDestination(t *testing.T) {
	output := filepath.Join(t.TempDir(), "snapshot.wapp")
	require.NoError(t, os.WriteFile(output, []byte("sentinel"), 0o644))
	cause := errors.New("injected integrity failure")

	_, err := writePackAtomically(output, nil, func(w io.Writer) error {
		_, writeErr := w.Write([]byte("complete but invalid pack"))
		return writeErr
	}, func(string) error { return cause })
	require.ErrorIs(t, err, cause)
	assertAtomicPackFailure(t, output, err)
}

func TestB13PackRejectsDirectOutputAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.wapp")
	require.NoError(t, os.WriteFile(path, []byte("sentinel"), 0o644))
	called := false

	_, err := writePackAtomically(path, []string{path}, func(io.Writer) error {
		called = true
		return nil
	}, func(string) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "aliases input")
	require.False(t, called)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, []byte("sentinel"), got)
}

func TestB14PackRejectsSymlinkOutputAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wapp")
	output := filepath.Join(dir, "output.wapp")
	require.NoError(t, os.WriteFile(input, []byte("sentinel"), 0o644))
	require.NoError(t, os.Symlink(input, output))
	called := false

	_, err := writePackAtomically(output, []string{input}, func(io.Writer) error {
		called = true
		return nil
	}, func(string) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "aliases input")
	require.False(t, called)
	got, readErr := os.ReadFile(input)
	require.NoError(t, readErr)
	require.Equal(t, []byte("sentinel"), got)
}
