// SPDX-License-Identifier: MPL-2.0

package affinity

import "context"

type partitionKey struct{}

// WithPartition stores a computed CPU partition in the context for downstream
// boot components (the actor host manager and the WASM function manager).
func WithPartition(ctx context.Context, p Partition) context.Context {
	return context.WithValue(ctx, partitionKey{}, p)
}

// PartitionFromContext returns the CPU partition stored in the context. The
// second result is false when no partition was set; the zero Partition is
// disabled and safe to use unconditionally.
func PartitionFromContext(ctx context.Context) (Partition, bool) {
	p, ok := ctx.Value(partitionKey{}).(Partition)
	return p, ok
}
