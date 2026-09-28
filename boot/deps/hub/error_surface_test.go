// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
		{name: "module already exists", err: ErrModuleAlreadyExists, kind: apierror.AlreadyExists},
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

// TestClientRefusalsAreTyped covers the conditions the client detects itself,
// before or after talking to the Hub; they sit on the same surface as the
// conditions the Hub reports.
func TestClientRefusalsAreTyped(t *testing.T) {
	t.Run("register without a base URL", func(t *testing.T) {
		_, err := (&Client{}).RegisterModule(t.Context(), &RegisterModuleParams{})
		var rich apierror.Error
		require.ErrorAs(t, err, &rich)
		require.Equal(t, apierror.Invalid, rich.Kind())
	})

	t.Run("publish without a token", func(t *testing.T) {
		_, err := (&Client{baseURL: "http://hub.invalid"}).PublishViaHub(t.Context(), UploadInput{})
		require.ErrorIs(t, err, ErrNotAuthenticated)
	})

	t.Run("publish answered without a publish id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"publish_id":""}`))
		}))
		defer srv.Close()
		client, err := NewClient(Options{BaseURL: srv.URL, Token: "tok"})
		require.NoError(t, err)

		_, err = client.PublishViaHub(t.Context(), UploadInput{
			Org: "acme", Module: "widgets", Version: "1.0.0", FilePath: wappFixture(t),
		})
		var rich apierror.Error
		require.ErrorAs(t, err, &rich)
		require.Equal(t, apierror.Internal, rich.Kind())
	})
}
