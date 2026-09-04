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

// Deliberately use no column affinity: types must come from each stored value,
// not schema declarations or UTF-8 guessing. Snapshot and both live images must
// agree, including empty values, embedded NULs and text-looking binary data.
func TestObserverStorageTypesAcrossSnapshotAndLive(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"text", "hello"}, {"unicode", "😃 大鹿"}, {"empty_text", ""},
		{"nul_text", "a\x00b"}, {"blob", []byte("hello")},
		{"empty_blob", []byte{}}, {"binary", []byte{0, 255, 128}},
		{"integer", int64(42)}, {"real", 1.25}, {"null", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := openObservedDB(t, filepath.Join(t.TempDir(), "types.db"))
			require.NoError(t, err)
			defer o.Close()
			_, err = o.opened.DB.Exec(`CREATE TABLE items(id INTEGER PRIMARY KEY, value); INSERT INTO items VALUES(0, ?)`, tc.value)
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
