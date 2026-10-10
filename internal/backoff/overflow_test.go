// SPDX-License-Identifier: MPL-2.0

package backoff

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestBackoffSaturatesBeforeDurationConversion(t *testing.T) {
	for _, cap := range []time.Duration{time.Minute, time.Duration(math.MaxInt64)} {
		t.Run(cap.String(), func(t *testing.T) {
			calc := NewCalculator(supervisor.RetryPolicy{
				InitialDelay: time.Second, BackoffFactor: math.MaxFloat64,
				MaxDelay: cap,
			})
			require.Equal(t, time.Second, calc.NextInterval())
			for range 10 {
				require.Equal(t, cap, calc.NextInterval())
			}
		})
	}
}

func TestBackoffJitterCannotOverflow(t *testing.T) {
	calc := NewCalculator(supervisor.RetryPolicy{
		InitialDelay: time.Duration(math.MaxInt64), BackoffFactor: 1, Jitter: .5,
	})
	for range 1000 {
		interval := calc.NextInterval()
		require.GreaterOrEqual(t, interval, time.Duration(math.MaxInt64/2), "overflow must not become an immediate retry")
	}
}

func TestBackoffNonFiniteValues(t *testing.T) {
	calc := NewCalculator(supervisor.RetryPolicy{InitialDelay: time.Second, BackoffFactor: math.NaN(), Jitter: math.NaN()})
	for range 3 {
		require.Equal(t, time.Second, calc.NextInterval())
	}
	calc = NewCalculator(supervisor.RetryPolicy{InitialDelay: time.Second, BackoffFactor: math.Inf(1), MaxDelay: time.Minute})
	calc.NextInterval()
	require.Equal(t, time.Minute, calc.NextInterval())
}
