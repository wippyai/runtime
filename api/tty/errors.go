// SPDX-License-Identifier: MPL-2.0

package tty

import "errors"

var (
	ErrServiceUnavailable  = errors.New("tty service unavailable")
	ErrInvalidGrant        = errors.New("invalid terminal grant")
	ErrInvalidPort         = errors.New("invalid terminal port")
	ErrInputInactive       = errors.New("terminal input is not started")
	ErrSurfaceOpen         = errors.New("terminal surface already open")
	ErrViewportClosed      = errors.New("viewport closed")
	ErrInvalidViewportSize = errors.New("invalid terminal viewport size")
)
