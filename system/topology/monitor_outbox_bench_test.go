// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

func BenchmarkMonitorOutboxAckMiss(b *testing.B) {
	target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
	caller := pid.PID{Node: "client", Host: "registry"}
	outbox, err := newMonitorOutbox(1, 1<<20)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if outbox.ack(target, caller, "already-settled") {
			b.Fatal("unexpected settlement")
		}
	}
}

func BenchmarkMonitorOutboxReserveFillAck(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{{"256B", 256}, {"64KiB", 64 << 10}} {
		b.Run(tc.name, func(b *testing.B) {
			target := pid.PID{Node: "server", Host: "process", UniqID: "actor"}
			caller := pid.PID{Node: "client", Host: "registry"}
			notice := make([]byte, tc.size)
			outbox, err := newMonitorOutbox(1, int64(tc.size+1024))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(tc.size))
			for b.Loop() {
				// This benchmark reuses a settled key to isolate storage cost; production
				// monitor references must follow the enclosing state machine's lifecycle.
				if err := outbox.reserve(target, caller, "reference", int64(tc.size)); err != nil {
					b.Fatal(err)
				}
				if _, err := outbox.fill(target, caller, "reference", notice); err != nil {
					b.Fatal(err)
				}
				if !outbox.ack(target, caller, "reference") {
					b.Fatal("missing obligation")
				}
			}
		})
	}
}
