package payload

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

func TestErrorInterface(t *testing.T) {
	cause := errors.New("test cause")
	err := luaapi.NewTranscodeError("test message", cause)

	t.Run("Error", func(t *testing.T) {
		assert.Equal(t, "test message: test cause", err.Error())
	})

	t.Run("Kind", func(t *testing.T) {
		assert.Equal(t, apierror.KindInternal, err.Kind())
	})

	t.Run("Retryable", func(t *testing.T) {
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("Details", func(t *testing.T) {
		assert.Nil(t, err.Details())
	})

	t.Run("Unwrap", func(t *testing.T) {
		assert.Equal(t, cause, err.Unwrap())
	})
}

func TestNewInvalidFormatError(t *testing.T) {
	err := luaapi.NewInvalidFormatError("invalid format")
	assert.Equal(t, "invalid format", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, err.Unwrap())
}

func TestNewInvalidTypeError(t *testing.T) {
	err := luaapi.NewInvalidTypeError("invalid type")
	assert.Equal(t, "invalid type", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, err.Unwrap())
}

func TestNewTranscodeError(t *testing.T) {
	cause := errors.New("underlying error")
	err := luaapi.NewTranscodeError("transcode failed", cause)
	assert.Equal(t, "transcode failed: underlying error", err.Error())
	assert.Equal(t, apierror.KindInternal, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Equal(t, cause, err.Unwrap())
}

func TestNewConversionError(t *testing.T) {
	cause := errors.New("conversion issue")
	err := luaapi.NewConversionError("conversion failed", cause)
	assert.Equal(t, "conversion failed: conversion issue", err.Error())
	assert.Equal(t, apierror.KindInternal, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Equal(t, cause, err.Unwrap())
}

func TestNewUnsupportedTypeError(t *testing.T) {
	err := luaapi.NewUnsupportedTypeError("unsupported type: custom")
	assert.Equal(t, "unsupported type: custom", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, err.Unwrap())
}
