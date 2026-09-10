// SPDX-License-Identifier: MPL-2.0
package terminal

import (
	"io"
	"testing"
)

func TestRetiredInputCleanupCannotStopRestartedSession(t *testing.T) {
	reader := NewEventInputReader(nil, io.Discard, nil, nil)
	retired := reader.Done()
	// Stop has completed and Start has published a new session before the old
	// read loop's deferred cleanup runs after its WaitGroup decrement.
	reader.done = make(chan struct{})
	reader.started = true
	active := reader.Done()
	if err := reader.stopWithCause(io.EOF, retired); err != nil {
		t.Fatal(err)
	}
	if !reader.started {
		t.Fatal("retired cleanup stopped new reader")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("retired error contaminated new reader: %v", err)
	}
	select {
	case <-active:
		t.Fatal("retired cleanup closed active completion")
	default:
	}
}
