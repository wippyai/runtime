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
		name  string
		value any
	}{
		{"text", "hello"}, {"unicode", "😃 大鹿"}, {"empty_text", ""},
		{"nul_text", "a\x00b"}, {"blob", []byte("hello")},
		{"empty_blob", []byte{}}, {"binary", []byte{0, 255, 128}},
		{"integer", int64(42)}, {"real", 1.25}, {"null", nil},
		{"DATE", "2026-09-04"}, {"DATETIME", "2026-09-04 12:30:00"}, {"BOOLEAN", int64(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := openObservedDB(t, filepath.Join(t.TempDir(), "types.db"))
			require.NoError(t, err)
			defer o.Close()
			declaration := ""
			switch tc.name {
			case "DATE", "DATETIME", "BOOLEAN":
				declaration = tc.name
			}
			_, err = o.opened.DB.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY, value `+declaration+`); INSERT INTO items VALUES(0, ?)`, tc.value)
			require.NoError(t, err)
			s, err := o.opened.Observer.Snapshot(context.Background(), sqlapi.SnapshotOptions{Tables: []string{"items"}})
			require.NoError(t, err)
			defer s.Close()
			snapshot := receiveBatch(t, s)
			require.Equal(t, tc.value, snapshot.Changes[0].After[1])
			_, err = o.opened.DB.Exec(`UPDATE items SET value=? WHERE id=0`, tc.value)
			require.NoError(t, err)
			live := receiveBatch(t, s)
			require.Equal(t, tc.value, live.Changes[0].Before[1])
			require.Equal(t, tc.value, live.Changes[0].After[1])
		})
	}
}
