// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func requireAPIError(t *testing.T, err error, kind apierror.Kind, msg string) apierror.Error {
	t.Helper()
	require.Error(t, err)
	var apiErr apierror.Error
	ok := errors.As(err, &apiErr)
	require.Truef(t, ok, "expected apierror.Error, got %T", err)
	assert.Equal(t, kind, apiErr.Kind())
	assert.Contains(t, err.Error(), msg)
	return apiErr
}

func assertDetailString(t *testing.T, apiErr apierror.Error, key, expected string) {
	t.Helper()
	assert.Equal(t, expected, apiErr.Details().GetString(key, ""))
}
