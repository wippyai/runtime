// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"testing"

	"github.com/jackc/pglogrepl"
)

func benchDecoder() *decoder {
	d := newDecoder()
	_, _ = d.apply(accountsRel(), 0)
	_, _ = d.apply(&pglogrepl.BeginMessage{FinalLSN: 0x10, Xid: 7}, 0)
	return d
}

func BenchmarkDecoderInsert(b *testing.B) {
	d := benchDecoder()
	msg := &pglogrepl.InsertMessage{RelationID: 42, Tuple: textTuple("1", "a@w.ai")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.apply(msg, 0x20); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecoderUpdate(b *testing.B) {
	d := benchDecoder()
	msg := &pglogrepl.UpdateMessage{
		RelationID: 42,
		OldTuple:   textTuple("1", "old@w.ai"),
		NewTuple:   textTuple("1", "new@w.ai"),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.apply(msg, 0x30); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTupleToMap(b *testing.B) {
	rel := accountsRel()
	tuple := textTuple("1", "a@w.ai")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tupleToMap(rel, tuple)
	}
}
