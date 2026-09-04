// SPDX-License-Identifier: MPL-2.0

// Package terminal provides terminal service configuration.
package terminal

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type testSurface struct {
	closeErr   error
	closeCount int
}

func (*testSurface) Present(frame ttyapi.Frame) (ttyapi.PresentStats, error) {
	return ttyapi.PresentStats{Rows: len(frame.Rows)}, nil
}
func (*testSurface) Invalidate()    {}
func (s *testSurface) Close() error { s.closeCount++; return s.closeErr }

func TestNewTerminalContext(t *testing.T) {
	stdin := bytes.NewBufferString("input")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	tc := NewTerminalContext(stdin, stdout, stderr)

	assert.NotNil(t, tc)
	assert.Equal(t, stdin, tc.Stdin)
	assert.Equal(t, stdout, tc.Stdout)
	assert.Equal(t, stderr, tc.Stderr)
}

func TestGetTerminalContext(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx := contextapi.NewRootContext()
		ctx, fc := contextapi.OpenFrameContext(ctx)

		tc := GetTerminalContext(ctx)
		assert.Nil(t, tc)

		stdin := bytes.NewBufferString("test")
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		termCtx := NewTerminalContext(stdin, stdout, stderr)

		err := fc.Set(terminalKey, termCtx)
		require.NoError(t, err)

		retrieved := GetTerminalContext(ctx)
		assert.Equal(t, termCtx, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := context.Background()

		tc := GetTerminalContext(ctx)
		assert.Nil(t, tc)
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := contextapi.NewRootContext()
		ctx, fc := contextapi.OpenFrameContext(ctx)

		err := fc.Set(terminalKey, "wrong type")
		require.NoError(t, err)

		tc := GetTerminalContext(ctx)
		assert.Nil(t, tc)
	})
}

func TestWithTerminalContext(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx := contextapi.NewRootContext()
		ctx, _ = contextapi.OpenFrameContext(ctx)

		stdin := bytes.NewBufferString("test")
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		termCtx := NewTerminalContext(stdin, stdout, stderr)

		err := WithTerminalContext(ctx, termCtx)
		require.NoError(t, err)

		retrieved := GetTerminalContext(ctx)
		assert.Equal(t, termCtx, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := context.Background()

		stdin := bytes.NewBufferString("test")
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		termCtx := NewTerminalContext(stdin, stdout, stderr)

		err := WithTerminalContext(ctx, termCtx)
		assert.Equal(t, contextapi.ErrNoFrameContext, err)
	})

	t.Run("sealed frame", func(t *testing.T) {
		ctx := contextapi.NewRootContext()
		ctx, fc := contextapi.OpenFrameContext(ctx)
		fc.Seal()

		stdin := bytes.NewBufferString("test")
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		termCtx := NewTerminalContext(stdin, stdout, stderr)

		err := WithTerminalContext(ctx, termCtx)
		assert.Error(t, err)
	})
}

func TestPipeContext(t *testing.T) {
	t.Run("stdin read", func(t *testing.T) {
		input := "hello world"
		stdin := bytes.NewBufferString(input)
		tc := NewTerminalContext(stdin, &bytes.Buffer{}, &bytes.Buffer{})

		buf := make([]byte, len(input))
		n, err := tc.Stdin.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, len(input), n)
		assert.Equal(t, input, string(buf))
	})

	t.Run("stdout write", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		tc := NewTerminalContext(nil, stdout, &bytes.Buffer{})

		output := "output data"
		n, err := tc.Stdout.Write([]byte(output))
		require.NoError(t, err)
		assert.Equal(t, len(output), n)
		assert.Equal(t, output, stdout.String())
	})

	t.Run("stderr write", func(t *testing.T) {
		stderr := &bytes.Buffer{}
		tc := NewTerminalContext(nil, &bytes.Buffer{}, stderr)

		errMsg := "error message"
		n, err := tc.Stderr.Write([]byte(errMsg))
		require.NoError(t, err)
		assert.Equal(t, len(errMsg), n)
		assert.Equal(t, errMsg, stderr.String())
	})
}

func TestPipeContextOwnsOnePresentationLease(t *testing.T) {
	tc := NewTerminalContext(nil, &bytes.Buffer{}, &bytes.Buffer{})
	tc.Surface = func(ttyapi.SurfaceOptions) (ttyapi.Surface, error) { return &testSurface{}, nil }

	first, err := tc.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	_, err = tc.OpenSurface(ttyapi.SurfaceOptions{})
	require.ErrorIs(t, err, ttyapi.ErrSurfaceOpen)
	require.NoError(t, first.Close())
	second, err := tc.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	require.NoError(t, second.Close())
	require.NoError(t, tc.Close())
	_, err = tc.OpenSurface(ttyapi.SurfaceOptions{})
	require.ErrorIs(t, err, ttyapi.ErrInvalidPort)
}

func TestPipeContextReleasesPresentationLeaseWhenCloseFails(t *testing.T) {
	closeErr := errors.New("close failed")
	tc := NewTerminalContext(nil, &bytes.Buffer{}, &bytes.Buffer{})
	var created []*testSurface
	tc.Surface = func(ttyapi.SurfaceOptions) (ttyapi.Surface, error) {
		backend := &testSurface{closeErr: closeErr}
		created = append(created, backend)
		return backend, nil
	}

	first, err := tc.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	require.ErrorIs(t, first.Close(), closeErr)
	require.ErrorIs(t, first.Close(), closeErr)
	require.Equal(t, 1, created[0].closeCount)
	second, err := tc.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	require.Len(t, created, 2)
	require.ErrorIs(t, second.Close(), closeErr)
}
