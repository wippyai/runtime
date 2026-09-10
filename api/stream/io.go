// SPDX-License-Identifier: MPL-2.0

package stream

import "context"

// ContextReader is an optional extension implemented by interruptible streams.
// ReadContext must return when ctx ends without retaining p after return.
// Implementations also provide io.Reader for ordinary native consumers.
type ContextReader interface {
	ReadContext(ctx context.Context, p []byte) (int, error)
}

// ContextWriter is an optional extension implemented by interruptible streams.
// WriteContext must return when ctx ends without retaining p after return.
// Cancellation does not undo bytes already written and must not trigger replay.
// Implementations also provide io.Writer for ordinary native producers.
type ContextWriter interface {
	WriteContext(ctx context.Context, p []byte) (int, error)
}
