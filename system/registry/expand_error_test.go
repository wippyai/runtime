// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/deps/hub"
)

func TestExpansionBoundariesPreserveDiagnostics(t *testing.T) {
	for _, kind := range []apierror.Kind{apierror.Invalid, apierror.Conflict, apierror.Unavailable, apierror.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			details := attrs.Bag{"count": 1, "errors": []string{"requirement app:audience has no value"}}
			cause := apierror.New(kind, "link diagnostic").WithRetryable(apierror.True).WithDetails(details)
			err := fmt.Errorf("directive: %w", cause)
			for _, wrap := range []func(error) apierror.Error{
				func(err error) apierror.Error { return build.NewStageError("link", err) },
				hub.NewDependencyPipelineError, NewExpandChangesError,
			} {
				err = wrap(err)
				var diagnostic apierror.Error
				require.ErrorAs(t, err, &diagnostic)
				require.Equal(t, kind, diagnostic.Kind())
				require.Equal(t, apierror.True, diagnostic.Retryable())
				require.Equal(t, details, diagnostic.Details())
				require.ErrorIs(t, err, cause)
				require.ErrorContains(t, err, "link diagnostic")
			}
		})
	}
	for _, wrap := range []func(error) apierror.Error{
		func(err error) apierror.Error { return build.NewStageError("link", err) },
		hub.NewDependencyPipelineError, NewExpandChangesError,
	} {
		cause := errors.New("unexpected failure")
		err := wrap(cause)
		require.Equal(t, apierror.Internal, err.Kind())
		require.ErrorIs(t, err, cause)
	}
}
