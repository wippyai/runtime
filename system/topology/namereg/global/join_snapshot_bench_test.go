// SPDX-License-Identifier: MPL-2.0

package global

import (
	"fmt"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/wippyai/runtime/api/pid"
)

func BenchmarkJoinSnapshotCapture(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			f := NewFSM()
			p := pid.PID{Node: "node", Host: "host", UniqID: "owner"}
			for i := 0; i < count; i++ {
				cmd := &Command{Type: CmdRegister, Name: fmt.Sprintf("name-%d", i), PID: p}
				if i%2 == 0 {
					cmd.Type = CmdRegisterPending
					cmd.RequiredNodes = []pid.NodeID{p.Node}
				}
				data, err := EncodeCommand(cmd)
				if err != nil {
					b.Fatal(err)
				}
				f.Apply(&hraft.Log{Data: data, Index: uint64(i + 1)})
			}
			s := &Service{fsm: f}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				snapshot, err := s.buildJoinSnapshot(0, count)
				if err != nil {
					b.Fatal(err)
				}
				if len(snapshot.Entries) != count {
					b.Fatal("incomplete snapshot")
				}
			}
		})
	}
}
