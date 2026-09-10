// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestConnectionRunAfterCloseReturns(t *testing.T) {
	a, b := newMockConnPair()
	defer b.Close()
	connection := newNodeConnection(a, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	connection.Close()
	done := make(chan *ConnectionError, 1)
	go func() { done <- connection.Run(func(Class, []byte) {}) }()
	select {
	case err := <-done:
		require.Equal(t, ExitCleanShutdown, err.Reason)
	case <-time.After(time.Second):
		// Release the leaked writer on the unfixed implementation so the
		// regression itself does not leave a background goroutine behind.
		connection.lifecycleMu.Lock()
		if connection.cancel != nil {
			connection.cancel()
		}
		connection.lifecycleMu.Unlock()
		<-done
		t.Fatal("Run created an uncanceled writer after Close")
	}
}
