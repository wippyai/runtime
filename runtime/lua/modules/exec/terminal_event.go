// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"errors"

	ttyapi "github.com/wippyai/runtime/api/tty"
)

var errTerminalInputFull = errors.New("terminal input queue is full")

// enqueueTerminalEvent never parks Lua behind a slow or exited child. Pointer
// motion is transient; discrete input reports backpressure.
func enqueueTerminalEvent(events chan ttyapi.Event, event ttyapi.Event) error {
	select {
	case events <- event:
		return nil
	default:
		if event.Type == "mouse" && event.Action == "motion" {
			return nil
		}
		return errTerminalInputFull
	}
}
