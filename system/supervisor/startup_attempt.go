// SPDX-License-Identifier: MPL-2.0

package supervisor

import "context"

type startupResult struct {
	ch  <-chan any
	err error
}

// startupAttempt keeps ownership until Start has returned and an abandoned
// success has been stopped. Worker results are read only after done closes.
type startupAttempt struct {
	result      chan startupResult
	decision    chan bool
	done        chan struct{}
	cleanupErr  error
	lateSuccess bool
	admitted    bool // lifecycle goroutine only
}

func (c *Controller) beginStartup(ctx context.Context) *startupAttempt {
	a := &startupAttempt{result: make(chan startupResult, 1), decision: make(chan bool, 1), done: make(chan struct{})}
	c.startAttempt = a
	c.startWG.Add(1)
	go func() {
		defer c.startWG.Done()
		defer func() {
			close(a.done)
			if a.cleanupErr != nil {
				select {
				case c.ops <- ctrlOp{kind: ctrlCleanupFailed, cleanup: a}:
				case <-c.ctx.Done():
				}
			}
		}()
		ch, err := c.service.Start(ctx)
		a.result <- startupResult{ch: ch, err: err}
		if admitted := <-a.decision; admitted || err != nil {
			return
		}
		a.lateSuccess = true
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.config.StopTimeout)
		defer cancel()
		a.cleanupErr = c.service.Stop(cleanup)
	}()
	return a
}

// Manual starts, like timed retries, wait for the previous attempt's owner.
// Waiting outside the lifecycle loop leaves Stop and its deadline responsive.
func (c *Controller) queueAfterStartup(op ctrlOp, pending *startupAttempt) {
	var done <-chan struct{}
	if op.ctx != nil {
		done = op.ctx.Done()
	}
	select {
	case <-pending.done:
		select {
		case c.ops <- op:
		case <-done:
			if op.result != nil {
				op.result <- op.ctx.Err()
			}
		case <-c.ctx.Done():
		}
	case <-done:
		if op.result != nil {
			op.result <- op.ctx.Err()
		}
	case <-c.ctx.Done():
	}
}
