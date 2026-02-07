package error

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
)

func TestBuildChain_Nil(t *testing.T) {
	chain := BuildChain(nil)
	assert.Nil(t, chain)
}

func TestBuildChain_PlainError(t *testing.T) {
	chain := BuildChain(errors.New("something broke"))
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)
	assert.Equal(t, "something broke", chain.Errors[0].Message)
	assert.Empty(t, chain.Errors[0].Kind)
	assert.Nil(t, chain.Errors[0].Retryable)
	assert.Nil(t, chain.Errors[0].Details)
	assert.Nil(t, chain.Errors[0].Stack)
}

func TestBuildChain_WrappedPlainErrors(t *testing.T) {
	inner := errors.New("inner")
	outer := errors.New("outer: " + inner.Error())
	// errors.New doesn't implement Unwrap, so chain has single element
	chain := BuildChain(outer)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)
}

func TestBuildChain_ApiError(t *testing.T) {
	e := New(NotFound, "resource missing").
		WithRetryable(False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": "abc"}))

	chain := BuildChain(e)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)

	ce := chain.Errors[0]
	assert.Equal(t, "resource missing", ce.Message)
	assert.Equal(t, "NotFound", ce.Kind)
	require.NotNil(t, ce.Retryable)
	assert.False(t, *ce.Retryable)
	assert.Equal(t, "abc", ce.Details["id"])
}

func TestBuildChain_ApiErrorWithCause(t *testing.T) {
	cause := errors.New("connection refused")
	e := New(Unavailable, "service down").
		WithRetryable(True).
		WithCause(cause)

	chain := BuildChain(e)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 2)

	// err.Error() includes cause: "service down: connection refused"
	assert.Equal(t, "service down: connection refused", chain.Errors[0].Message)
	assert.Equal(t, "Unavailable", chain.Errors[0].Kind)
	require.NotNil(t, chain.Errors[0].Retryable)
	assert.True(t, *chain.Errors[0].Retryable)

	assert.Equal(t, "connection refused", chain.Errors[1].Message)
	assert.Empty(t, chain.Errors[1].Kind)
}

func TestBuildChain_RichError(t *testing.T) {
	e := NewRich(Internal, "panic recovered").
		WithRetryable(False).
		WithDetails(map[string]any{"goroutine": 42}).
		WithStack([]string{"handler.go:15 in doWork", "main.go:5"})

	chain := BuildChain(e)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)

	ce := chain.Errors[0]
	assert.Equal(t, "panic recovered", ce.Message)
	assert.Equal(t, "Internal", ce.Kind)
	require.NotNil(t, ce.Retryable)
	assert.False(t, *ce.Retryable)
	assert.Equal(t, 42, ce.Details["goroutine"])
	assert.Equal(t, []string{"handler.go:15 in doWork", "main.go:5"}, ce.Stack)
}

func TestBuildChain_RichErrorWithCause(t *testing.T) {
	inner := NewRich(NotFound, "item missing").
		WithRetryable(False)
	outer := NewRich(Internal, "handler failed").
		WithCause(inner)

	chain := BuildChain(outer)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 2)

	assert.Equal(t, "handler failed", chain.Errors[0].Message)
	assert.Equal(t, "Internal", chain.Errors[0].Kind)

	assert.Equal(t, "item missing", chain.Errors[1].Message)
	assert.Equal(t, "NotFound", chain.Errors[1].Kind)
}

func TestBuildChain_UnknownKindOmitted(t *testing.T) {
	e := New(Unknown, "generic error")
	chain := BuildChain(e)
	require.NotNil(t, chain)
	assert.Empty(t, chain.Errors[0].Kind)
}

func TestBuildChain_UnspecifiedRetryableOmitted(t *testing.T) {
	e := New(Internal, "error")
	chain := BuildChain(e)
	require.NotNil(t, chain)
	assert.Nil(t, chain.Errors[0].Retryable)
}

func TestBuildChain_EmptyDetailsOmitted(t *testing.T) {
	e := New(Internal, "error").WithDetails(attrs.NewBag())
	chain := BuildChain(e)
	require.NotNil(t, chain)
	assert.Nil(t, chain.Errors[0].Details)
}

// stackError implements StackProvider on a plain error
type stackError struct {
	msg   string
	stack []string
}

func (e *stackError) Error() string         { return e.msg }
func (e *stackError) StackFrames() []string { return e.stack }

func TestBuildChain_StackProvider(t *testing.T) {
	e := &stackError{
		msg:   "lua error",
		stack: []string{"script.lua:10 in main"},
	}
	chain := BuildChain(e)
	require.NotNil(t, chain)
	assert.Equal(t, []string{"script.lua:10 in main"}, chain.Errors[0].Stack)
}

// --- Chain.Root ---

func TestChain_Root_Nil(t *testing.T) {
	var c *Chain
	assert.Nil(t, c.Root())
}

func TestChain_Root_Empty(t *testing.T) {
	c := &Chain{Errors: []ChainedError{}}
	assert.Nil(t, c.Root())
}

func TestChain_Root_ReturnsFirst(t *testing.T) {
	c := &Chain{Errors: []ChainedError{
		{Message: "outer"},
		{Message: "inner"},
	}}
	root := c.Root()
	require.NotNil(t, root)
	assert.Equal(t, "outer", root.Message)
}

// --- FromChain ---

func TestFromChain_Nil(t *testing.T) {
	assert.Nil(t, FromChain(nil))
}

func TestFromChain_Empty(t *testing.T) {
	assert.Nil(t, FromChain(&Chain{Errors: []ChainedError{}}))
}

func TestFromChain_SingleError(t *testing.T) {
	retryable := true
	chain := &Chain{Errors: []ChainedError{
		{
			Message:   "not found",
			Kind:      "NotFound",
			Retryable: &retryable,
			Details:   map[string]any{"id": "xyz"},
			Stack:     []string{"handler.lua:5"},
		},
	}}

	result := FromChain(chain)
	require.NotNil(t, result)
	assert.Equal(t, "not found", result.Msg())
	assert.Equal(t, "not found", result.Error())
	assert.Equal(t, Kind("NotFound"), result.Kind())
	assert.Equal(t, True, result.Retryable())
	assert.Equal(t, "xyz", result.Details()["id"])
	assert.Equal(t, []string{"handler.lua:5"}, result.StackFrames())
	assert.Nil(t, result.Unwrap())
}

func TestFromChain_RetryableFalse(t *testing.T) {
	retryable := false
	chain := &Chain{Errors: []ChainedError{
		{Message: "permanent", Retryable: &retryable},
	}}

	result := FromChain(chain)
	require.NotNil(t, result)
	assert.Equal(t, False, result.Retryable())
}

func TestFromChain_RetryableUnspecified(t *testing.T) {
	chain := &Chain{Errors: []ChainedError{
		{Message: "unknown retry"},
	}}

	result := FromChain(chain)
	require.NotNil(t, result)
	assert.Equal(t, Unspecified, result.Retryable())
}

func TestFromChain_NestedChain(t *testing.T) {
	retryable := false
	chain := &Chain{Errors: []ChainedError{
		{Message: "outer", Kind: "Internal"},
		{Message: "middle", Kind: "Unavailable", Retryable: &retryable},
		{Message: "root cause"},
	}}

	result := FromChain(chain)
	require.NotNil(t, result)

	// Outermost
	assert.Equal(t, "outer", result.Msg())
	assert.Equal(t, Kind("Internal"), result.Kind())

	// Middle
	var middle *RichError
	require.ErrorAs(t, result.Unwrap(), &middle)
	assert.Equal(t, "middle", middle.Msg())
	assert.Equal(t, Kind("Unavailable"), middle.Kind())
	assert.Equal(t, False, middle.Retryable())

	// Innermost
	var inner *RichError
	require.ErrorAs(t, middle.Unwrap(), &inner)
	assert.Equal(t, "root cause", inner.Msg())
	assert.Nil(t, inner.Unwrap())
}

// --- Roundtrip: BuildChain -> FromChain ---

func TestChain_Roundtrip_ApiError(t *testing.T) {
	original := New(PermissionDenied, "access denied").
		WithRetryable(False).
		WithDetails(attrs.NewBagFrom(map[string]any{"resource": "/admin"})).
		WithCause(errors.New("missing token"))

	chain := BuildChain(original)
	require.NotNil(t, chain)

	restored := FromChain(chain)
	require.NotNil(t, restored)

	// The err type uses Error() (includes cause) for the chain message
	assert.Equal(t, "access denied: missing token", restored.Msg())
	assert.Equal(t, Kind("PermissionDenied"), restored.Kind())
	assert.Equal(t, False, restored.Retryable())
	assert.Equal(t, "/admin", restored.Details()["resource"])

	cause := restored.Unwrap()
	require.NotNil(t, cause)
	var richCause *RichError
	require.ErrorAs(t, cause, &richCause)
	assert.Equal(t, "missing token", richCause.Msg())
}

func TestChain_Roundtrip_RichError(t *testing.T) {
	original := NewRich(Timeout, "deadline exceeded").
		WithRetryable(True).
		WithStack([]string{"worker.lua:42", "scheduler.lua:10"}).
		WithDetails(map[string]any{"elapsed_ms": 5000})

	chain := BuildChain(original)
	restored := FromChain(chain)
	require.NotNil(t, restored)

	assert.Equal(t, "deadline exceeded", restored.Msg())
	assert.Equal(t, Kind("Timeout"), restored.Kind())
	assert.Equal(t, True, restored.Retryable())
	assert.Equal(t, []string{"worker.lua:42", "scheduler.lua:10"}, restored.StackFrames())
	assert.Equal(t, 5000, restored.Details()["elapsed_ms"])
}

func TestFromChain_ErrorString_ChainsCauseMessages(t *testing.T) {
	chain := &Chain{Errors: []ChainedError{
		{Message: "outer"},
		{Message: "inner"},
	}}

	result := FromChain(chain)
	assert.Equal(t, "outer: inner", result.Error())
}
