// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBusDoneTracksShutdown(t *testing.T) {
	bus := NewBus()
	done := bus.Done()
	require.NotNil(t, done)
	select {
	case <-done:
		t.Fatal("new bus is already stopped")
	default:
	}
	bus.Stop()
	select {
	case <-done:
	default:
		t.Fatal("stopped bus did not release lifecycle waiters")
	}
	bus.Stop()
	require.Equal(t, done, bus.Done())
}
