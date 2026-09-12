// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestStrongRegistrationDeadlineHonorsCaller(t *testing.T) {
	now := time.Now()
	for _, offset := range []time.Duration{-time.Second, time.Millisecond, 49 * time.Millisecond, time.Second, time.Hour} {
		ctx, cancel := context.WithDeadline(context.Background(), now.Add(offset))
		got := strongRegistrationDeadline(ctx, now, 10*time.Second)
		cancel()
		require.Equal(t, now.Add(min(offset, 10*time.Second)), got)
	}
	require.Equal(t, now.Add(time.Second), strongRegistrationDeadline(context.Background(), now, time.Second))
}

func TestStrongTimingValidation(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	policy := StrongTiming{AckDeadline: time.Minute, RetryInterval: time.Millisecond}
	require.NoError(t, r.ConfigureStrongTiming(policy), "zero result grace is supported")
	for _, bad := range []StrongTiming{
		{AckDeadline: 0, RetryInterval: time.Second},
		{AckDeadline: time.Second, RetryInterval: 0},
		{AckDeadline: time.Second, RetryInterval: time.Second, ResultWaitGrace: -time.Second},
	} {
		require.Error(t, r.ConfigureStrongTiming(bad))
		require.Equal(t, policy.AckDeadline, r.strong.deadline, "invalid policy must not partially mutate configuration")
		require.Equal(t, policy.ResultWaitGrace, r.strong.resultWaitGrace)
		require.Equal(t, policy.RetryInterval, r.strong.retryInterval)
	}
	require.Error(t, (&Service{}).ConfigureStrongTiming(policy))
}
