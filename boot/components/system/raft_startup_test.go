// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRaftStartupObservesLeaderWithoutConsumingLeadershipNotifications(t *testing.T) {
	calls := 0
	require.NoError(t, waitRaftLeader(context.Background(), func() bool {
		calls++
		return calls == 2 // A follower learns a leader without becoming leader itself.
	}, time.Second, time.Millisecond))
	require.Equal(t, 2, calls)
}

func TestRaftStartupHonorsCancellationAndMissingLeader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, waitRaftLeader(ctx, func() bool { t.Error("canceled startup queried leader"); return true }, time.Second, time.Millisecond), context.Canceled)
	require.ErrorIs(t, waitRaftLeader(context.Background(), func() bool { return false }, time.Millisecond, time.Hour), context.DeadlineExceeded)
	require.Error(t, waitRaftLeader(context.Background(), func() bool { return true }, time.Second, 0))
}
