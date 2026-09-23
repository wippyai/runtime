// SPDX-License-Identifier: MPL-2.0

package kv

import apierror "github.com/wippyai/runtime/api/error"

var (
	// ErrSnapshotTooLarge means the requested key set or detached reply exceeds
	// the bounded authority-snapshot envelope.
	ErrSnapshotTooLarge = apierror.New(apierror.Invalid, "authority snapshot exceeds its bounds").WithRetryable(apierror.False)
	// ErrSnapshotInvalid means the authority-snapshot request or reply was not
	// a canonical, strictly decodable envelope.
	ErrSnapshotInvalid = apierror.New(apierror.Invalid, "invalid authority snapshot").WithRetryable(apierror.False)
	// ErrSnapshotBusy means the bounded authority-snapshot admission budget is
	// currently exhausted. Callers may retry with the same request.
	ErrSnapshotBusy = apierror.New(apierror.RateLimited, "authority snapshot capacity exhausted").WithRetryable(apierror.True)
	// ErrSnapshotUnavailable means the transport or authority changed while a
	// snapshot was being forwarded. Callers may retry after resolving again.
	ErrSnapshotUnavailable = apierror.New(apierror.Unavailable, "authority snapshot temporarily unavailable").WithRetryable(apierror.True)
)
