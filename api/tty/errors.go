// SPDX-License-Identifier: MPL-2.0

package tty

import "errors"

var (
	ErrPermissionDenied    = errors.New("terminal mount permission denied")
	ErrMountExpired        = errors.New("terminal mount expired or revoked")
	ErrMeshBusy            = errors.New("terminal mesh capacity exceeded")
	ErrMeshProtocol        = errors.New("invalid terminal mesh frame")
	ErrServiceUnavailable  = errors.New("tty service unavailable")
	ErrInvalidGrant        = errors.New("invalid terminal grant")
	ErrInvalidPort         = errors.New("invalid terminal port")
	ErrInputInactive       = errors.New("terminal input is not started")
	ErrSurfaceOpen         = errors.New("terminal surface already open")
	ErrViewportClosed      = errors.New("viewport closed")
	ErrInvalidViewportSize = errors.New("invalid terminal viewport size")
)
