package events

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// NewConfigDataRequiredError creates an error when configuration data is missing
func NewConfigDataRequiredError() apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"configuration data is required for create/update operations",
		apierror.False,
		nil,
		nil,
	)
}

// NewUnknownEventKindError creates an error for unknown event kind
func NewUnknownEventKindError(kind string) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"unknown event kind: "+kind,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"event_kind": kind}),
		nil,
	)
}
