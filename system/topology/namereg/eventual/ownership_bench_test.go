// SPDX-License-Identifier: MPL-2.0
package eventual

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

// Different names exercise admission parallelism independently of conflicts.
// The service still performs actual state mutation and broadcast admission.
func BenchmarkConcurrentDistinctNameRegistration(b *testing.B) {
	s := NewService(Config{LocalNodeID: "local"})
	owner := pid.PID{Node: "local", Host: "process", UniqID: "owner"}
	var next atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			name := strconv.FormatUint(next.Add(1), 10)
			if _, err := s.Register(name, owner); err != nil {
				b.Fatal(err)
			}
		}
	})
}
