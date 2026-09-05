// SPDX-License-Identifier: MPL-2.0

package socket

import "errors"

var (
	ErrNilOperation    = errors.New("socket: nil pending operation")
	ErrAlreadyStarted  = errors.New("socket: pending operation already started")
	ErrOperationClosed = errors.New("socket: pending operation closed")
	ErrAlreadyTaken    = errors.New("socket: pending operation result already taken")
	ErrNoResult        = errors.New("socket: pending operation completed without a result")
)
