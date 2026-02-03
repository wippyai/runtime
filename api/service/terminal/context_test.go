// Package terminal provides terminal service configuration.
package terminal

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
)

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
