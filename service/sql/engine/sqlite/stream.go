// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"strings"
	"sync"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

type mutationStream struct {
	err           error
	ctx           context.Context
	changes       chan sqlapi.MutationBatch
	notify        chan struct{}
	done          chan struct{}
	cancel        context.CancelFunc
	backend       *sqliteBackend
	watermark     string
	queue         []sqlapi.MutationBatch
	pending       []sqlapi.MutationBatch
	opts          sqlapi.MutationOptions
	queuedChanges int
	queuedBytes   int
	maxChanges    int
	maxBytes      int
	mu            sync.Mutex
	snapshotting  bool
	closed        bool
}

func newMutationStream(ctx context.Context, backend *sqliteBackend, opts sqlapi.MutationOptions) *mutationStream {
	if ctx == nil {
		ctx = context.Background()
	}
	stream := &mutationStream{
		ctx:        ctx,
		backend:    backend,
		opts:       opts,
		changes:    make(chan sqlapi.MutationBatch),
		notify:     make(chan struct{}, 1),
		done:       make(chan struct{}),
		maxChanges: opts.MaxChanges,
		maxBytes:   opts.MaxBytes,
	}
	return stream
}

func (s *mutationStream) start() {
	go s.relay()
}

func newSnapshotStream(ctx context.Context, backend *sqliteBackend, opts sqlapi.SnapshotOptions, watermark string, cancel context.CancelFunc) *mutationStream {
	stream := newMutationStream(ctx, backend, sqlapi.MutationOptions{
		Tables: opts.Tables, MaxChanges: opts.MaxChanges, MaxBytes: opts.MaxBytes,
	})
	stream.snapshotting = true
	stream.watermark = watermark
	stream.cancel = cancel
	return stream
}

func (s *mutationStream) Changes() <-chan sqlapi.MutationBatch { return s.changes }

func (s *mutationStream) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *mutationStream) Close() error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.backend.remove(s, nil)
	return nil
}

func (s *mutationStream) push(batch sqlapi.MutationBatch) {
	batch = filterBatch(batch, s.opts)
	if len(batch.Changes) == 0 {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	overflow := false
	if s.snapshotting {
		if !s.enqueuePendingLocked(batch) {
			s.closeLocked(errObserverOverflow)
			overflow = true
		}
	} else if !s.enqueueLocked(batch) {
		s.closeLocked(errObserverOverflow)
		overflow = true
	}
	s.mu.Unlock()
	if overflow {
		s.backend.remove(s, errObserverOverflow)
	}
}

func filterBatch(batch sqlapi.MutationBatch, opts sqlapi.MutationOptions) sqlapi.MutationBatch {
	if len(opts.Tables) == 0 && len(opts.Operations) == 0 {
		return batch
	}
	filtered := make([]sqlapi.Mutation, 0, len(batch.Changes))
	for _, change := range batch.Changes {
		if len(opts.Tables) > 0 && !matchesTable(change.Schema, change.Table, opts.Tables) {
			continue
		}
		if len(opts.Operations) > 0 && !matchesValue(change.Op, opts.Operations) {
			continue
		}
		filtered = append(filtered, change)
	}
	batch.Changes = filtered
	return batch
}

func matchesTable(schema, table string, filters []string) bool {
	for _, filter := range filters {
		if filter == table || filter == schema+"."+table {
			return true
		}
	}
	return false
}

func matchesValue(value string, filters []string) bool {
	for _, filter := range filters {
		if strings.EqualFold(value, filter) {
			return true
		}
	}
	return false
}

func (s *mutationStream) pushSnapshot(batch sqlapi.MutationBatch) error {
	s.mu.Lock()
	if s.closed {
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return errObserverClosed
	}
	if !s.enqueueLocked(batch) {
		s.closeLocked(errObserverOverflow)
		s.mu.Unlock()
		s.backend.remove(s, errObserverOverflow)
		return errObserverOverflow
	}
	s.mu.Unlock()
	return nil
}

func (s *mutationStream) finishSnapshot(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.closeLocked(err)
		s.mu.Unlock()
		s.backend.remove(s, err)
		return
	}
	s.snapshotting = false
	s.queue = append(s.queue, s.pending...)
	s.pending = nil
	s.signalLocked()
	s.mu.Unlock()
}

func (s *mutationStream) Watermark() string { return s.watermark }

func (s *mutationStream) enqueueLocked(batch sqlapi.MutationBatch) bool {
	changes := len(batch.Changes)
	bytes := mutationBatchBytes(batch)
	if s.maxChanges > 0 && (changes > s.maxChanges || s.queuedChanges > s.maxChanges-changes) {
		return false
	}
	if s.maxBytes > 0 && (bytes > s.maxBytes || s.queuedBytes > s.maxBytes-bytes) {
		return false
	}
	s.queue = append(s.queue, batch)
	s.queuedChanges = saturatingAdd(s.queuedChanges, changes)
	s.queuedBytes = saturatingAdd(s.queuedBytes, bytes)
	s.signalLocked()
	return true
}

func (s *mutationStream) enqueuePendingLocked(batch sqlapi.MutationBatch) bool {
	changes := len(batch.Changes)
	bytes := mutationBatchBytes(batch)
	if s.maxChanges > 0 && (changes > s.maxChanges || s.queuedChanges > s.maxChanges-changes) {
		return false
	}
	if s.maxBytes > 0 && (bytes > s.maxBytes || s.queuedBytes > s.maxBytes-bytes) {
		return false
	}
	s.pending = append(s.pending, batch)
	s.queuedChanges = saturatingAdd(s.queuedChanges, changes)
	s.queuedBytes = saturatingAdd(s.queuedBytes, bytes)
	s.signalLocked()
	return true
}

func (s *mutationStream) signalLocked() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *mutationStream) relay() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			closed := s.closed
			s.mu.Unlock()
			if closed {
				close(s.changes)
				return
			}
			select {
			case <-s.notify:
			case <-s.ctx.Done():
				s.backend.remove(s, s.ctx.Err())
				close(s.changes)
				return
			}
			continue
		}
		batch := s.queue[0]
		s.mu.Unlock()

		select {
		case s.changes <- batch:
		case <-s.done:
			close(s.changes)
			return
		case <-s.ctx.Done():
			s.backend.remove(s, s.ctx.Err())
			close(s.changes)
			return
		}

		s.mu.Lock()
		if len(s.queue) > 0 {
			s.queue = s.queue[1:]
			s.queuedChanges -= len(batch.Changes)
			s.queuedBytes -= mutationBatchBytes(batch)
		}
		s.mu.Unlock()
	}
}

func mutationBatchBytes(batch sqlapi.MutationBatch) int {
	bytes := saturatingAdd(mutationStructuralBytes, len(batch.Transaction))
	for _, change := range batch.Changes {
		bytes = saturatingAdd(bytes, mutationSize(change))
	}
	return bytes
}

func mutationSize(change sqlapi.Mutation) int {
	bytes := mutationStructuralBytes
	bytes = saturatingAdd(bytes, len(change.Schema))
	bytes = saturatingAdd(bytes, len(change.Table))
	bytes = saturatingAdd(bytes, len(change.Op))
	for _, column := range change.Columns {
		bytes = saturatingAdd(bytes, len(column))
	}
	bytes = saturatingAdd(bytes, mutationValuesBytes(change.Before))
	return saturatingAdd(bytes, mutationValuesBytes(change.After))
}

func mutationValuesBytes(values []any) int {
	bytes := 0
	for _, value := range values {
		bytes = saturatingAdd(bytes, valueStructuralBytes)
		switch value := value.(type) {
		case nil:
			bytes = saturatingAdd(bytes, 1)
		case []byte:
			bytes = saturatingAdd(bytes, len(value))
		case string:
			bytes = saturatingAdd(bytes, len(value))
		default:
			bytes = saturatingAdd(bytes, 16)
		}
	}
	return bytes
}

func saturatingAdd(left, right int) int {
	if left < 0 || right < 0 {
		return int(^uint(0) >> 1)
	}
	maxInt := int(^uint(0) >> 1)
	if left > maxInt-right {
		return maxInt
	}
	return left + right
}

func (s *mutationStream) closeWithError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked(err)
}

func (s *mutationStream) closeLocked(err error) {
	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	close(s.done)
	s.queue = nil
	s.pending = nil
	s.queuedChanges = 0
	s.queuedBytes = 0
	s.signalLocked()
}
