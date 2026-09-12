// SPDX-License-Identifier: MPL-2.0
package internode

import "testing"

var measuredIdleQueues []*classQueue

// Isolates queue storage for the9,900 managed-peer states in a100-node mesh.
// It excludes TLS, goroutine stacks and other state; use benchtime=1x for the
// eager-storage baseline, which allocates hundreds of MiB in one iteration.
func BenchmarkIdlePeerQueueStorage(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		queues := make([]*classQueue, 0, 2*100*99)
		for range 100 * 99 {
			queues = append(queues, newClassQueue(1024), newClassQueue(64))
		}
		measuredIdleQueues = queues
	}
}
