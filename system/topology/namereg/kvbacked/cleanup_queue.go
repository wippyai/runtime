// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/pid"
)

var ErrCleanupIncomplete = errors.New("naming cleanup remains incomplete")
var errCleanupCapacity = errors.New("naming cleanup capacity exhausted")

// CleanupConfig bounds retained owner identities, not exit payloads/results.
// RetryInterval is a retry policy, never evidence that a process has died.
type CleanupConfig struct {
	MaxPending    int
	MaxBytes      int
	BatchSize     int
	RetryInterval time.Duration
}

func DefaultCleanupConfig() CleanupConfig {
	return CleanupConfig{MaxPending: 256, MaxBytes: 1 << 20, BatchSize: 32, RetryInterval: time.Second}
}
func (c *CleanupConfig) initDefaults() {
	defaults := DefaultCleanupConfig()
	if c.MaxPending == 0 {
		c.MaxPending = defaults.MaxPending
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = defaults.MaxBytes
	}
	if c.BatchSize == 0 {
		c.BatchSize = min(defaults.BatchSize, c.MaxPending)
	}
	if c.RetryInterval == 0 {
		c.RetryInterval = defaults.RetryInterval
	}
}
func (c CleanupConfig) validate() error {
	if c.MaxPending <= 0 || c.MaxBytes <= 0 || c.BatchSize <= 0 || c.BatchSize > c.MaxPending || c.RetryInterval <= 0 {
		return errors.New("invalid naming cleanup limits")
	}
	return nil
}

type cleanupJob struct {
	owner pid.PID
	key   string
	bytes int
	leave func()
}

// cleanupQueue retains each mutation lease until success or explicit aborted
// shutdown. Thus participant retirement joins accepted cleanup before sealing
// the name guard/committing retirement. An aborted stop reports pending work;
// this in-memory queue deliberately makes no crash-durability promise.
type cleanupQueue struct {
	mu               sync.Mutex
	service          *Service
	config           CleanupConfig
	jobs             map[string]*cleanupJob
	ready, failed    []*cleanupJob
	bytes            int
	wake             chan struct{}
	done             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	started, stopped bool
	stopErr          error
	owner            *reconcilerLifecycle
	stopOwnerWatch   func() bool
}

func newCleanupQueue(s *Service, config CleanupConfig) *cleanupQueue {
	return &cleanupQueue{service: s, config: config, jobs: make(map[string]*cleanupJob), wake: make(chan struct{}, 1), done: make(chan struct{})}
}
func (q *cleanupQueue) start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped || q.started {
		return context.Canceled
	}
	q.ctx, q.cancel = context.WithCancel(ctx)
	q.started = true
	go q.run()
	return nil
}
func (q *cleanupQueue) enqueue(ctx context.Context, owner pid.PID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	leave, err := q.service.admitMutation(ctx)
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			leave()
		}
	}()
	key := owner.String()
	cost := len(key) + len(owner.Node) + len(owner.Host) + len(owner.UniqID)
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.started || q.stopped || q.ctx.Err() != nil {
		return context.Canceled
	}
	if _, exists := q.jobs[key]; exists {
		return nil
	}
	if len(q.jobs) >= q.config.MaxPending || cost > q.config.MaxBytes-q.bytes {
		return errCleanupCapacity
	}
	if run := q.service.reconciler.Load(); run != nil {
		if q.owner != nil && q.owner != run {
			return context.Canceled
		}
		if q.owner == nil {
			q.owner = run
			q.stopOwnerWatch = context.AfterFunc(run.ctx, q.cancel)
		}
	}
	job := &cleanupJob{owner: owner, key: key, bytes: cost, leave: leave}
	q.jobs[key] = job
	q.ready = append(q.ready, job)
	q.bytes += cost
	retained = true
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}
func (q *cleanupQueue) take() []*cleanupJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(len(q.ready), q.config.BatchSize)
	if n == 0 {
		return nil
	}
	batch := q.ready[:n:n]
	q.ready = q.ready[n:]
	if len(q.ready) == 0 {
		q.ready = nil
	}
	return batch
}
func (q *cleanupQueue) finish(batch []*cleanupJob, results map[string]error) bool {
	var release []func()
	q.mu.Lock()
	for _, job := range batch {
		err, reported := results[job.key]
		if !reported || err != nil {
			q.failed = append(q.failed, job)
			continue
		}
		delete(q.jobs, job.key)
		q.bytes -= job.bytes
		q.service.monitored.Delete(job.key)
		release = append(release, job.leave)
	}
	failed := len(q.failed) != 0
	q.mu.Unlock()
	for _, leave := range release {
		leave()
	}
	return failed
}
func (q *cleanupQueue) run() {
	var timer *time.Timer
	var retry <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
		q.mu.Lock()
		q.stopped = true
		pending := q.jobs
		if len(pending) != 0 {
			q.stopErr = fmt.Errorf("%w: %d owners", ErrCleanupIncomplete, len(pending))
		}
		// Preserve unfinished identities for inspection/recovery by the owning
		// composition. Their mutation leases must be released to join forced stop.
		q.ready, q.failed = nil, nil
		var releases []func()
		for _, job := range pending {
			releases = append(releases, job.leave)
			job.leave = nil
		}
		stopWatch := q.stopOwnerWatch
		q.mu.Unlock()
		if stopWatch != nil {
			stopWatch()
		}
		for _, leave := range releases {
			leave()
		}
		close(q.done)
	}()
	for {
		if q.ctx.Err() != nil {
			return
		}
		if retry != nil {
			select {
			case <-retry:
				retry = nil
				timer = nil
				q.mu.Lock()
				q.ready = append(q.ready, q.failed...)
				q.failed = nil
				q.mu.Unlock()
			default:
			}
		}
		batch := q.take()
		if len(batch) != 0 {
			owners := make([]pid.PID, len(batch))
			for i, job := range batch {
				owners[i] = job.owner
			}
			failed := q.finish(batch, q.service.reapOwners(q.ctx, owners))
			if failed && retry == nil {
				timer = time.NewTimer(q.config.RetryInterval)
				retry = timer.C
			}
			continue
		}
		select {
		case <-q.ctx.Done():
			return
		case <-q.wake:
		case <-retry:
			retry = nil
			timer = nil
			q.mu.Lock()
			q.ready = append(q.ready, q.failed...)
			q.failed = nil
			q.mu.Unlock()
		}
	}
}
func (q *cleanupQueue) stop(ctx context.Context) error {
	q.mu.Lock()
	if !q.started {
		if !q.stopped {
			q.stopped = true
			close(q.done)
		}
	} else {
		q.cancel()
	}
	q.mu.Unlock()
	select {
	case <-q.done:
		q.mu.Lock()
		defer q.mu.Unlock()
		return q.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
