// SPDX-License-Identifier: MPL-2.0

package clock

import "errors"

// Errors returned by clock handlers.
var (
	ErrTimerNotFound  = errors.New("timer not found")
	ErrTickerNotFound = errors.New("ticker not found")
)
