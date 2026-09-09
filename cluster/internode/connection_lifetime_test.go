// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestConnectionClosureObservationSurvivesSubscriptionRaces(t *testing.T) {
	a, b := newMockConnPair()
	defer b.Close()
	connection := newNodeConnection(a, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	signal := connection.Closed()
	require.Equal(t, signal, connection.Closed())
	var joined sync.WaitGroup
	for range 100 {
		joined.Go(func() { <-connection.Closed() })
	}
	connection.Close()
	joined.Wait()
	select {
	case <-signal:
	default:
		t.Fatal("connection closure was not signaled")
	}
	require.Equal(t, signal, connection.Closed(), "late subscribers must observe the same closed lifetime")
	connection.Close()
}
func TestConnectionClosedBeforeFirstSubscriber(t *testing.T) {
	a, b := newMockConnPair()
	defer b.Close()
	connection := newNodeConnection(a, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	connection.Close()
	select {
	case <-connection.Closed():
	default:
		t.Fatal("late subscription missed closure")
	}
}
