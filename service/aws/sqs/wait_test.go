// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The receive loop must not spin on a context-oblivious sleep after a receive
// error; waitWithContext is the helper we use and it must return immediately
// on cancellation.
func TestWaitWithContext_CancelsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lifecycle := make(chan struct{})
	start := time.Now()
	completed := waitWithContext(ctx, lifecycle, time.Second)
	elapsed := time.Since(start)

	assert.False(t, completed, "must report cancellation, not timer completion")
	assert.Less(t, elapsed, 50*time.Millisecond, "must return promptly on ctx cancel")
}

func TestWaitWithContext_LifecycleCancels(t *testing.T) {
	ctx := context.Background()
	lifecycle := make(chan struct{})
	close(lifecycle)

	start := time.Now()
	completed := waitWithContext(ctx, lifecycle, time.Second)
	elapsed := time.Since(start)

	assert.False(t, completed)
	assert.Less(t, elapsed, 50*time.Millisecond)
}

func TestWaitWithContext_TimerCompletes(t *testing.T) {
	ctx := context.Background()
	lifecycle := make(chan struct{})

	start := time.Now()
	completed := waitWithContext(ctx, lifecycle, 20*time.Millisecond)
	elapsed := time.Since(start)

	assert.True(t, completed, "timer-driven return must report completion")
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
}
