// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

type releasedPayload struct {
	Data   any
	Format payload.Format
}

type releasedEntry struct {
	Meta           attrs.Bag
	Data           *releasedPayload
	ID             registry.ID
	Kind           string
	DependencyRoot bool `codec:"DependencyRoot,omitempty"`
}

type releasedRecord struct {
	Module  string
	Version string
	Digest  string
	Root    bool
}

type releasedOperation struct {
	OriginalEntry *releasedEntry  `codec:"OriginalEntry"`
	Current       *releasedRecord `codec:"prov,omitempty"`
	Previous      *releasedRecord `codec:"oprov,omitempty"`
	Kind          string          `codec:"Kind"`
	Entry         releasedEntry   `codec:"Entry"`
}

func TestMigrateEntryMetadata_RewritesAllHistoryBranches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	history, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })

	id := registry.NewID("app", "dependency")
	other := registry.NewID("app", "other")
	seedReleasedChangeset(t, history, 1, 0, []releasedOperation{{
		Kind: registry.EntryCreate,
		Entry: releasedEntry{
			ID:             id,
			Kind:           registry.NamespaceDependency,
			Meta:           attrs.NewBagFrom(map[string]any{"module": "active/module", "module_version": "1.0.0", "module_digest": "sha256:old", "author_key": "preserved"}),
			DependencyRoot: true,
		},
		Current: &releasedRecord{Module: "active/module", Version: "1.0.0", Digest: "sha256:released", Root: true},
	}})
	seedReleasedChangeset(t, history, 2, 1, []releasedOperation{{
		Kind:  registry.EntryUpdate,
		Entry: releasedEntry{ID: id, Kind: registry.NamespaceDependency},
		OriginalEntry: &releasedEntry{
			ID: id, Kind: registry.NamespaceDependency,
		},
		Current: &releasedRecord{Module: "active/module", Root: false},
	}})
	seedReleasedChangeset(t, history, 3, 2, []releasedOperation{{
		Kind:    registry.EntryDelete,
		Entry:   releasedEntry{ID: id, Kind: registry.NamespaceDependency},
		Current: &releasedRecord{Module: "active/module"},
	}})
	// v4 is a sibling of v1, proving every branch is converted rather than only
	// the current head lineage.
	seedReleasedChangeset(t, history, 4, 1, []releasedOperation{{
		Kind:  registry.EntryCreate,
		Entry: releasedEntry{ID: other, Kind: registry.EntryKind, Meta: attrs.NewBagFrom(map[string]any{"module": "stamped/only"})},
	}})

	baseline := registry.State{{
		ID:       id,
		Kind:     registry.NamespaceDependency,
		Registry: registry.EntryMetadata{Owner: "active/module", Root: true},
	}}
	require.NoError(t, MigrateEntryMetadata(context.Background(), history, baseline))
	var migrationVersion string
	require.NoError(t, history.db.QueryRowContext(t.Context(), `SELECT curr_version FROM schema_version WHERE name = 'registry_history.entry_metadata'`).Scan(&migrationVersion))
	require.Equal(t, "1.1", migrationVersion)
	var migrationAudits int
	require.NoError(t, history.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_update_history WHERE name = 'registry_history.entry_metadata'`).Scan(&migrationAudits))
	require.Equal(t, 1, migrationAudits)

	v1 := version.FromParent(version.New(0), 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v2, 3)
	v4 := version.FromParent(v1, 4)
	first, err := history.Get(v1)
	require.NoError(t, err)
	require.Equal(t, registry.EntryMetadata{Owner: "active/module", Root: true}, first[0].Entry.Registry)
	require.NotContains(t, first[0].Entry.Meta, "module")
	require.NotContains(t, first[0].Entry.Meta, "module_version")
	require.NotContains(t, first[0].Entry.Meta, "module_digest")
	require.Equal(t, "preserved", first[0].Entry.Meta["author_key"])

	second, err := history.Get(v2)
	require.NoError(t, err)
	require.Equal(t, registry.EntryMetadata{Owner: "active/module"}, second[0].Entry.Registry)
	require.NotNil(t, second[0].OriginalEntry)
	require.Equal(t, registry.EntryMetadata{Owner: "active/module", Root: true}, second[0].OriginalEntry.Registry, "rollback input inherits the active baseline root when no persisted root state exists")

	third, err := history.Get(v3)
	require.NoError(t, err)
	require.Equal(t, registry.EntryMetadata{Owner: "active/module"}, third[0].Entry.Registry, "delete rollback input keeps the active baseline root")

	branch, err := history.Get(v4)
	require.NoError(t, err)
	require.Equal(t, registry.EntryMetadata{Owner: "stamped/only"}, branch[0].Entry.Registry)
	require.NotContains(t, branch[0].Entry.Meta, "module")

	before := changesetBytes(t, history, 1)
	require.NoError(t, history.Close())
	history, err = NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	// A later deployment baseline must not rewrite already-normalized history.
	changedBaseline := append(registry.State(nil), baseline...)
	changedBaseline[0].Registry.Owner = "next/module"
	require.NoError(t, MigrateEntryMetadata(context.Background(), history, changedBaseline))
	require.Equal(t, before, changesetBytes(t, history, 1), "second boot must not rewrite normalized history")
	first, err = history.Get(v1)
	require.NoError(t, err)
	require.Equal(t, "active/module", first[0].Entry.Registry.Owner)
}

func TestMigrateEntryMetadata_RejectsConflictingOwnerAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	history, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { require.NoError(t, history.Close()) }()

	id := registry.NewID("app", "entry")
	seedReleasedChangeset(t, history, 1, 0, []releasedOperation{{
		Kind:    registry.EntryCreate,
		Entry:   releasedEntry{ID: id, Kind: registry.EntryKind},
		Current: &releasedRecord{Module: "released/module"},
	}})
	before := changesetBytes(t, history, 1)
	err = MigrateEntryMetadata(context.Background(), history, registry.State{{
		ID: id, Kind: registry.EntryKind, Registry: registry.EntryMetadata{Owner: "active/module"},
	}})
	require.ErrorContains(t, err, "conflicting owners")
	require.Equal(t, before, changesetBytes(t, history, 1), "failed migration must not write any history row")
	var count int
	require.NoError(t, history.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_version WHERE name = 'registry_history.entry_metadata'`).Scan(&count))
	require.Zero(t, count, "failed migration must not advance its ledger")
}

func TestMigrateEntryMetadata_ConcurrentOpenersAdvanceLedgerOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	first, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { require.NoError(t, first.Close()) }()
	second, err := NewSQLite(path, zap.NewNop())
	require.NoError(t, err)
	defer func() { require.NoError(t, second.Close()) }()

	id := registry.NewID("app", "entry")
	seedReleasedChangeset(t, first, 1, 0, []releasedOperation{{
		Kind:    registry.EntryCreate,
		Entry:   releasedEntry{ID: id, Kind: registry.EntryKind},
		Current: &releasedRecord{Module: "active/module"},
	}})
	baseline := registry.State{{ID: id, Kind: registry.EntryKind, Registry: registry.EntryMetadata{Owner: "active/module"}}}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, history := range []*History{first, second} {
		wait.Add(1)
		go func(history *History) {
			defer wait.Done()
			<-start
			errs <- MigrateEntryMetadata(context.Background(), history, baseline)
		}(history)
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var count int
	require.NoError(t, first.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_update_history WHERE name = 'registry_history.entry_metadata'`).Scan(&count))
	require.Equal(t, 1, count)
}

func seedReleasedChangeset(t *testing.T, history *History, id, parent uint, operations []releasedOperation) {
	t.Helper()
	var data bytes.Buffer
	encoder := codec.NewEncoder(&data, history.handle)
	require.NoError(t, encoder.Encode(operations))
	_, err := history.db.ExecContext(t.Context(), `INSERT INTO versions (id, parent_id) VALUES (?, ?)`, id, parent)
	require.NoError(t, err)
	_, err = history.db.ExecContext(t.Context(), `INSERT INTO changesets (version_id, data) VALUES (?, ?)`, id, data.Bytes())
	require.NoError(t, err)
}

func changesetBytes(t *testing.T, history *History, id uint) []byte {
	t.Helper()
	var data []byte
	require.NoError(t, history.db.QueryRowContext(t.Context(), `SELECT data FROM changesets WHERE version_id = ?`, id).Scan(&data))
	return data
}
