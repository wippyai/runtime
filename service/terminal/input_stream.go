// SPDX-License-Identifier: MPL-2.0

package terminal

import "io"

// terminalInputReader is the cancellable native input source owned by an
// InputReader session.
type terminalInputReader interface {
	io.Reader
	Cancel() bool
	Close() error
}

type inputEventSink func(*TTYEvent)
type graphicsEventSink func(id int, payload []byte) bool
