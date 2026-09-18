// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

// TestHubConditionsCarryKindAndStayMatchable proves the package reports every
// Hub condition on one surface: a caller can read the kind without parsing a
// message, and can still match the condition with errors.Is.
func TestHubConditionsCarryKindAndStayMatchable(t *testing.T) {
	for _, test := range []struct {
		err  error
		name string
		kind apierror.Kind
	}{
		{name: "not authenticated", err: ErrNotAuthenticated, kind: apierror.PermissionDenied},
		{name: "org access denied", err: ErrOrgAccessDenied, kind: apierror.PermissionDenied},
		{name: "module not found", err: ErrModuleNotFound, kind: apierror.NotFound},
		{name: "version exists", err: ErrVersionExists, kind: apierror.AlreadyExists},
		{name: "invalid version", err: ErrInvalidVersion, kind: apierror.Invalid},
		{name: "digest mismatch", err: ErrDigestMismatch, kind: apierror.Invalid},
		{name: "upload expired", err: ErrUploadExpired, kind: apierror.Invalid},
		{name: "publish in progress", err: ErrPublishInProgress, kind: apierror.Conflict},
		{name: "quota exceeded", err: ErrQuotaExceeded, kind: apierror.RateLimited},
		{name: "hub unavailable", err: ErrHubUnavailable, kind: apierror.Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rich apierror.Error
			require.ErrorAs(t, test.err, &rich)
			require.Equal(t, test.kind, rich.Kind())
			require.ErrorIs(t, test.err, test.err)
		})
	}
}

// TestVariantConditionsKeepTheirSentinelAndKind covers the two conditions whose
// message varies with what the Hub reported. They must stay matchable against
// the fixed sentinel while carrying their own detail.
func TestVariantConditionsKeepTheirSentinelAndKind(t *testing.T) {
	unavailable := &UnavailableError{RetryAfter: "1", Detail: "overloaded"}
	require.ErrorIs(t, unavailable, ErrHubUnavailable)
	require.Equal(t, "hub rate limited the request, retry in 1s", unavailable.Error())

	var rich apierror.Error
	require.ErrorAs(t, error(unavailable), &rich)
	require.Equal(t, apierror.Unavailable, rich.Kind())
	require.Equal(t, apierror.True, rich.Retryable())
	require.Equal(t, "1", rich.Details().GetString("retry_after", ""))
	require.Equal(t, "overloaded", rich.Details().GetString("detail", ""))

	quota := &QuotaExceededError{Reason: "seat limit"}
	require.ErrorIs(t, quota, ErrQuotaExceeded)
	require.Equal(t, "quota exceeded: seat limit", quota.Error())

	require.ErrorAs(t, error(quota), &rich)
	require.Equal(t, apierror.RateLimited, rich.Kind())
	require.Equal(t, "seat limit", rich.Details().GetString("reason", ""))
}

// TestWrappedConditionsStayMatchable proves a condition survives the wrapping
// the package does around it.
func TestWrappedConditionsStayMatchable(t *testing.T) {
	wrapped := NewArtifactIOError("materialize recorded module", "acme/worker", ErrModuleNotFound)
	require.ErrorIs(t, wrapped, ErrModuleNotFound)

	var rich apierror.Error
	require.True(t, errors.As(wrapped, &rich))
	require.Equal(t, "acme/worker", rich.Details().GetString("target", ""))
}
