// SPDX-License-Identifier: MPL-2.0

package clock

import "errors"

// Errors returned by clock handlers.
var (
	ErrTimerNotFound  = errors.New("timer not found")
	ErrTickerNotFound = errors.New("ticker not found")

	// ErrStoppedBeforeStart is returned by TimerStart / TickerStart when
	// the dispatcher has already received a matching TimerStopByChID /
	// TickerStopByChID for the same (pid, epoch, chID) triple. The Go
	// timer is not scheduled.
	ErrStoppedBeforeStart = errors.New("clock: stop arrived before start")
)
