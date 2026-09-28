// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"context"
	"testing"
)

func TestDispatcherStopBeforeStart(t *testing.T) {
	// Boot must clean up successfully loaded components even if a later Load
	// fails before the Start phase reaches this dispatcher.
	d := NewDispatcher()
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
