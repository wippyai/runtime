// SPDX-License-Identifier: MPL-2.0

package sql

import "context"

// CommittedMutationSource is an optional capability exposed by a SQL resource.
// It reports mutations observed by the database engine after a transaction
// boundary. The interface deliberately contains no driver-specific handles or
// SQL connection types; consumers can use the same capability with different
// database engines.
//
// A source is owned by the SQL resource generation that created it. Closing the
// generation closes the source and all of its streams.
type CommittedMutationSource interface {
	Subscribe(context.Context, MutationOptions) (MutationStream, error)
	// Snapshot returns a consistent read view followed by committed mutations
	// after its watermark, without a gap between the two. Engines own the
	// synchronization mechanism; no database handle is exposed to consumers.
	Snapshot(context.Context, SnapshotOptions) (SnapshotStream, error)
	Close() error
}

// MutationOptions controls an observation stream. Filtering is intentionally
// expressed in the neutral mutation vocabulary so a consumer does not need to
// know which SQL driver produced the stream.
type MutationOptions struct {
	Tables     []string
	Operations []string
	MaxChanges int
	// MaxBytes bounds retained logical mutation bytes. A native SQLite row is
	// materialized before the observer can reject it, so one row may
	// transiently exceed this conservative bound.
	MaxBytes int
}

// SnapshotOptions selects tables and the maximum number of rows in one
// snapshot batch. A non-positive BatchSize uses the engine default.
type SnapshotOptions struct {
	Tables     []string
	BatchSize  int
	MaxChanges int
	// MaxBytes bounds retained logical snapshot/live bytes. A native SQLite
	// row is materialized before the observer can reject it, so one row may
	// transiently exceed this conservative bound.
	MaxBytes int
}

// MutationStream delivers committed mutation batches in commit order.
type MutationStream interface {
	Changes() <-chan MutationBatch
	Err() error
	Close() error
}

// SnapshotStream is an atomic snapshot/live handoff. Snapshot batches are
// marked with MutationBatch.Snapshot; once they are exhausted, the stream
// carries live batches in commit order. Watermark identifies the fence at
// which the read view was established and is process-local unless an engine
// documents a durable position.
type SnapshotStream interface {
	MutationStream
	Watermark() string
}

// MutationBatch is the delivery unit emitted by an observation source. A live
// batch belongs to one committed database transaction. Snapshot batches are
// bounded chunks of a read view, not independent database transactions.
// Empty batches are not emitted.
type MutationBatch struct {
	Transaction string
	Changes     []Mutation
	Snapshot    bool
}

// Mutation is a driver-neutral row mutation. Values retain the database/sql
// driver's native scalar representation. Column names are captured with the
// mutation so schema changes after capture cannot relabel an earlier row.
type Mutation struct {
	Schema  string
	Table   string
	Op      string
	Columns []string
	Before  []any
	After   []any
}
