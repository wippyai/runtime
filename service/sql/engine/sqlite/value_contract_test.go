// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestReconciliationPreservesStorageType(t *testing.T) {
	require.False(t, mutationValuesEqual([]any{"same"}, []any{[]byte("same")}))
	require.False(t, mutationValuesEqual([]any{int64(1)}, []any{true}))
	require.True(t, mutationValuesEqual([]any{[]byte("same")}, []any{[]byte("same")}))
}

// Most cases deliberately use no column affinity: types must come from each
// stored value, not UTF-8 guessing. DATE/BOOLEAN cases also ensure query-driver
// conversions cannot change snapshots relative to live images.
func TestObserverStorageTypesAcrossSnapshotAndLive(t *testing.T) {
	for _, tc := range []struct {
		value any
		name  string
	}{
		{name: "text", value: "hello"}, {name: "unicode", value: "😃 大鹿"}, {name: "empty_text", value: ""},
		{name: "nul_text", value: "a\x00b"}, {name: "blob", value: []byte("hello")},
		{name: "empty_blob", value: []byte{}}, {name: "binary", value: []byte{0, 255, 128}},
		{name: "integer", value: int64(42)}, {name: "real", value: 1.25}, {name: "null", value: nil},
		{name: "DATE", value: "2026-09-04"}, {name: "DATETIME", value: "2026-09-04 12:30:00"}, {name: "BOOLEAN", value: int64(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			o, err := openObservedDB(t, filepath.Join(t.TempDir(), "types.db"))
			require.NoError(t, err)
			defer o.Close()
			declaration := ""
			switch tc.name {
			case "DATE", "DATETIME", "BOOLEAN":
				declaration = tc.name
			}
			_, err = o.opened.DB.ExecContext(ctx, `CREATE TABLE items(id INTEGER PRIMARY KEY, value `+declaration+`); INSERT INTO items VALUES(0, ?)`, tc.value)
			require.NoError(t, err)
			s, err := o.opened.Observer.Snapshot(ctx, sqlapi.SnapshotOptions{Tables: []string{"items"}})
			require.NoError(t, err)
			defer s.Close()
			snapshot := receiveBatch(t, s)
			require.Equal(t, tc.value, snapshot.Changes[0].After[1])
			_, err = o.opened.DB.ExecContext(ctx, `UPDATE items SET value=? WHERE id=0`, tc.value)
			require.NoError(t, err)
			live := receiveBatch(t, s)
			require.Equal(t, tc.value, live.Changes[0].Before[1])
			require.Equal(t, tc.value, live.Changes[0].After[1])
		})
	}
}
