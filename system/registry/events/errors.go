// SPDX-License-Identifier: MPL-2.0

package events

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// NewConfigDataRequiredError creates an error when configuration data is missing
func NewConfigDataRequiredError() apierror.Error {
	return apierror.New(apierror.Invalid, "configuration data is required for create/update operations").
		WithRetryable(apierror.False)
}

// NewUnknownEventKindError creates an error for unknown event kind
func NewUnknownEventKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown event kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"event_kind": kind}))
}
