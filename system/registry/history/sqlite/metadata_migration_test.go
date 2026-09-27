// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/internal/version"
	syspayload "github.com/wippyai/runtime/system/payload"
	payloadjson "github.com/wippyai/runtime/system/payload/json"
	migrationstorage "github.com/wippyai/runtime/system/registry/migration/storage"
	"go.uber.org/zap"
)

// This is the released JSON-tagged provenance wire shape, as stored in C.
type lowercaseOwnership struct {
	Module  string `codec:"module,omitempty"`
	Version string `codec:"version,omitempty"`
	Digest  string `codec:"digest,omitempty"`
	Root    bool   `codec:"root,omitempty"`
}

type lowercaseOperation struct {
	Kind          string              `codec:"Kind"`
	Entry         releasedEntry       `codec:"Entry"`
	OriginalEntry *releasedEntry      `codec:"OriginalEntry"`
	Current       *lowercaseOwnership `codec:"prov,omitempty"`
	Previous      *lowercaseOwnership `codec:"oprov,omitempty"`
}

func TestMigrateEntryMetadata_LowercaseOwnershipPreservesRootAndParameters(t *testing.T) {
	history, err := NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })
	id := registry.NewID("app.deps", "keeper")
	parameters := `{"component":"keeper/keeper","version":"0.5.75","parameters":[{"name":"keeper:admin_scope","value":"app.security:admin"}]}`
	seedAnyChangeset(t, history, 1, 0, []lowercaseOperation{{
		Kind: registry.EntryCreate,
		Entry: releasedEntry{ID: id, Kind: registry.NamespaceDependency,
			Data: &releasedPayload{Data: parameters, Format: payload.JSON}},
		Current: &lowercaseOwnership{Module: "kickside/kickside", Version: "0.1.128", Digest: "sha256:root", Root: true},
	}})
	seedAnyChangeset(t, history, 2, 1, []lowercaseOperation{{
		Kind:  registry.EntryUpdate,
		Entry: releasedEntry{ID: id, Kind: registry.NamespaceDependency},
		OriginalEntry: &releasedEntry{ID: id, Kind: registry.NamespaceDependency,
			Data: &releasedPayload{Data: parameters, Format: payload.JSON}},
		Current:  &lowercaseOwnership{Module: "kickside/kickside"},
		Previous: &lowercaseOwnership{Module: "kickside/kickside", Root: true},
	}})
	baseline := registry.State{{ID: id, Kind: registry.NamespaceDependency,
		Registry: registry.EntryMetadata{Owner: "kickside/kickside", Root: true}}}
	require.NoError(t, MigrateEntryMetadata(t.Context(), history, baseline))
	changes, err := history.Get(version.FromParent(version.New(0), 1))
	require.NoError(t, err)
	require.Equal(t, registry.EntryMetadata{Owner: "kickside/kickside", Root: true}, changes[0].Entry.Registry)
	require.Equal(t, parameters, changes[0].Entry.Data.Data())
	// Boot replays the saved operation over its deployment baseline. Feed the
	// restored root into the same link stage used by boot.
	restored := baseline[0]
	require.NoError(t, history.ReplayChanges(t.Context(), version.FromParent(version.New(0), 1),
		func(cs registry.ChangeSet) error {
			for _, op := range cs {
				if op.Entry.ID.Equal(id) {
					restored = op.Entry
				}
			}
			return nil
		}))
	require.True(t, restored.Registry.Root)
	entries := []registry.Entry{restored,
		{ID: registry.NewID("keeper", "definition"), Kind: registry.NamespaceDefinition,
			Registry: registry.EntryMetadata{Owner: "keeper/keeper"}},
		{ID: registry.NewID("keeper", "admin_scope"), Kind: registry.NamespaceRequirement,
			Registry: registry.EntryMetadata{Owner: "keeper/keeper"},
			Data:     payload.NewPayload(`{"targets":[{"entry":"service","path":".data.admin_scope"}]}`, payload.JSON)},
		{ID: registry.NewID("keeper", "service"), Kind: "process.lua",
			Data: payload.NewPayload(`{}`, payload.JSON)},
	}
	transcoder := syspayload.NewTranscoder()
	payloadjson.Register(transcoder)
	ctx := payload.WithTranscoder(ctxapi.WithAppContext(t.Context(), ctxapi.NewAppContext()), transcoder)
	require.NoError(t, stages.Link(stages.WithDependencies([]registry.Entry{restored}),
		stages.WithStrictRequirementModules([]string{"keeper/keeper"})).Execute(ctx, &entries))
	require.Equal(t, "app.security:admin", entries[3].Data.Data().(map[string]any)["admin_scope"])
	updated, err := history.Get(version.FromParent(version.FromParent(version.New(0), 1), 2))
	require.NoError(t, err)
	require.False(t, updated[0].Entry.Registry.Root, "explicit lowercase false remains false")
	require.True(t, updated[0].OriginalEntry.Registry.Root, "lowercase oprov root survives rollback input")
}

func TestMigrateEntryMetadata_RejectsUnknownOwnershipAtomically(t *testing.T) {
	history, err := NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })
	id := registry.NewID("app.deps", "keeper")
	seedAnyChangeset(t, history, 1, 0, []map[string]any{{
		"Kind":  registry.EntryCreate,
		"Entry": releasedEntry{ID: id, Kind: registry.NamespaceDependency},
		"prov":  map[string]any{"unknown": true},
	}})
	before := changesetBytes(t, history, 1)
	err = MigrateEntryMetadata(t.Context(), history, registry.State{{ID: id,
		Registry: registry.EntryMetadata{Owner: "kickside/kickside", Root: true}}})
	var decodeErr *migrationstorage.OwnershipDecodeError
	require.True(t, errors.As(err, &decodeErr), "expected typed ownership error: %v", err)
	require.Equal(t, before, changesetBytes(t, history, 1))
}

func TestMigrateEntryMetadata_RejectsNullOwnership(t *testing.T) {
	history, err := NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })
	id := registry.NewID("app.deps", "keeper")
	seedAnyChangeset(t, history, 1, 0, []map[string]any{{
		"Kind":  registry.EntryCreate,
		"Entry": releasedEntry{ID: id, Kind: registry.NamespaceDependency},
		"prov":  nil,
	}})
	err = MigrateEntryMetadata(t.Context(), history, nil)
	var decodeErr *migrationstorage.OwnershipDecodeError
	require.True(t, errors.As(err, &decodeErr), "expected typed ownership error: %v", err)
}

func TestMigrateEntryMetadata_RejectsContradictoryOwnershipSpellings(t *testing.T) {
	cases := []struct {
		name  string
		field string
		raw   map[string]any
	}{
		{"module_lower_empty", "prov", map[string]any{"module": "", "Module": "org/mod"}},
		{"module_upper_empty", "prov", map[string]any{"module": "org/mod", "Module": ""}},
		{"version_lower_empty", "prov", map[string]any{"version": "", "Version": "1.0.0"}},
		{"version_upper_empty", "prov", map[string]any{"version": "1.0.0", "Version": ""}},
		{"digest_lower_empty", "prov", map[string]any{"digest": "", "Digest": "sha256:abc"}},
		{"digest_upper_empty", "prov", map[string]any{"digest": "sha256:abc", "Digest": ""}},
		{"root_disagrees", "prov", map[string]any{"root": false, "Root": true}},
		{"previous_module_disagrees", "oprov", map[string]any{"module": "", "Module": "org/mod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history, err := NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, history.Close()) })
			id := registry.NewID("app.deps", "keeper")
			op := map[string]any{"Kind": registry.EntryCreate,
				"Entry": encodedEntry{ID: id, Kind: registry.NamespaceDependency}}
			op[tc.field] = tc.raw
			seedAnyChangeset(t, history, 1, 0, []map[string]any{op})
			before := changesetBytes(t, history, 1)
			err = MigrateEntryMetadata(t.Context(), history, nil)
			var decodeErr *migrationstorage.OwnershipDecodeError
			require.True(t, errors.As(err, &decodeErr), "expected typed error: %v", err)
			require.Equal(t, tc.field, decodeErr.Field)
			require.Equal(t, before, changesetBytes(t, history, 1))
			var ledgerCount int
			require.NoError(t, history.db.QueryRowContext(t.Context(),
				`SELECT COUNT(*) FROM schema_version WHERE name = 'registry_history.entry_metadata'`).Scan(&ledgerCount))
			require.Zero(t, ledgerCount)
		})
	}
}

func TestMigrateEntryMetadata_OwnerConflictsAreTyped(t *testing.T) {
	id := registry.NewID("app.deps", "keeper")
	cases := []struct {
		name     string
		entry    encodedEntry
		original *encodedEntry
		current  map[string]any
		previous map[string]any
		baseline registry.State
	}{
		{name: "current_record_vs_entry", entry: encodedEntry{ID: id,
			Registry: registry.EntryMetadata{Owner: "org/entry"}}, current: map[string]any{"module": "org/record"}},
		{name: "previous_record_vs_original", entry: encodedEntry{ID: id},
			original: &encodedEntry{ID: id, Registry: registry.EntryMetadata{Owner: "org/original"}},
			previous: map[string]any{"module": "org/record"}},
		{name: "current_vs_previous", entry: encodedEntry{ID: id},
			original: &encodedEntry{ID: id}, current: map[string]any{"module": "org/current"},
			previous: map[string]any{"module": "org/previous"}},
		{name: "record_vs_baseline", entry: encodedEntry{ID: id},
			current:  map[string]any{"module": "org/record"},
			baseline: registry.State{{ID: id, Registry: registry.EntryMetadata{Owner: "org/baseline"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history, err := NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, history.Close()) })
			op := map[string]any{"Kind": registry.EntryCreate, "Entry": tc.entry}
			if tc.original != nil {
				op["OriginalEntry"] = tc.original
			}
			if tc.current != nil {
				op["prov"] = tc.current
			}
			if tc.previous != nil {
				op["oprov"] = tc.previous
			}
			seedAnyChangeset(t, history, 1, 0, []map[string]any{op})
			before := changesetBytes(t, history, 1)
			err = MigrateEntryMetadata(t.Context(), history, tc.baseline)
			var decodeErr *migrationstorage.OwnershipDecodeError
			require.True(t, errors.As(err, &decodeErr), "expected typed error: %v", err)
			require.Equal(t, id.String(), decodeErr.ID.String())
			require.ErrorContains(t, err, "conflicting owners")
			require.Equal(t, before, changesetBytes(t, history, 1))
		})
	}
}

func TestMigrateEntryMetadata_AlreadyMigratedFalseRootIsAmbiguous(t *testing.T) {
	history, err := NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })
	id := registry.NewID("app.deps", "keeper")
	// This normalized 1.1 row is byte-identical whether its original prov.root
	// was legitimately false or the old decoder dropped a lowercase true.
	seedAnyChangeset(t, history, 1, 0, []encodedOperation{{
		Kind: registry.EntryCreate,
		Entry: encodedEntry{ID: id, Kind: registry.NamespaceDependency,
			Registry: registry.EntryMetadata{Owner: "kickside/kickside"}},
	}})
	_, err = history.db.ExecContext(t.Context(), `INSERT INTO schema_version
		(name, curr_version, min_compatible_version, updated_at)
		VALUES ('registry_history.entry_metadata', '1.1', '1.0', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	before := changesetBytes(t, history, 1)
	require.NoError(t, MigrateEntryMetadata(t.Context(), history, registry.State{{
		ID: id, Registry: registry.EntryMetadata{Owner: "kickside/kickside", Root: true},
	}}))
	require.Equal(t, before, changesetBytes(t, history, 1),
		"a baseline root cannot distinguish a lost true bit from an explicit false bit")
}

func seedAnyChangeset(t *testing.T, history *History, id, parent uint, operations any) {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, codec.NewEncoder(&data, history.handle).Encode(operations))
	_, err := history.db.ExecContext(t.Context(), `INSERT INTO versions (id, parent_id) VALUES (?, ?)`, id, parent)
	require.NoError(t, err)
	_, err = history.db.ExecContext(t.Context(), `INSERT INTO changesets (version_id, data) VALUES (?, ?)`, id, data.Bytes())
	require.NoError(t, err)
}

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
