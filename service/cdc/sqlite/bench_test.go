// SPDX-License-Identifier: MPL-2.0

package sqlite

import "testing"

func BenchmarkMapRow(b *testing.B) {
	cols := []columnInfo{
		{name: "id"},
		{name: "email", text: true},
		{name: "balance"},
		{name: "blob"},
	}
	vals := []any{int64(1), []byte("user@example.com"), 42.5, []byte{0x00, 0x01, 0x02}}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = mapRow(cols, vals)
	}
}
