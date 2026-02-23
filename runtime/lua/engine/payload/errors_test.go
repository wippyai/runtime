// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
)

func TestErrorInterface(t *testing.T) {
	cause := errors.New("test cause")
	err := runtimelua.NewTranscodeError("test message", cause)

	t.Run("Error", func(t *testing.T) {
		assert.Equal(t, "test message: test cause", err.Error())
	})

	t.Run("Kind", func(t *testing.T) {
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("Retryable", func(t *testing.T) {
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("Details", func(t *testing.T) {
		assert.Nil(t, err.Details())
	})

	t.Run("Unwrap", func(t *testing.T) {
		assert.Equal(t, cause, errors.Unwrap(err))
	})
}

func TestNewInvalidFormatError(t *testing.T) {
	err := runtimelua.NewInvalidFormatError("invalid format")
	assert.Equal(t, "invalid format", err.Error())
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, errors.Unwrap(err))
}

func TestNewInvalidTypeError(t *testing.T) {
	err := runtimelua.NewInvalidTypeError("invalid type")
	assert.Equal(t, "invalid type", err.Error())
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, errors.Unwrap(err))
}

func TestNewTranscodeError(t *testing.T) {
	cause := errors.New("underlying error")
	err := runtimelua.NewTranscodeError("transcode failed", cause)
	assert.Equal(t, "transcode failed: underlying error", err.Error())
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestNewConversionError(t *testing.T) {
	cause := errors.New("conversion issue")
	err := runtimelua.NewConversionError("conversion failed", cause)
	assert.Equal(t, "conversion failed: conversion issue", err.Error())
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestNewUnsupportedTypeError(t *testing.T) {
	err := runtimelua.NewUnsupportedTypeError("unsupported type: custom")
	assert.Equal(t, "unsupported type: custom", err.Error())
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, errors.Unwrap(err))
}
