// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	_ "github.com/mattn/go-sqlite3" // Register SQLite3 database driver
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historyschema "github.com/wippyai/runtime/system/registry/history/schema"
	schemamanager "github.com/wippyai/runtime/system/schema"
	"go.uber.org/zap"
)

type History struct {
	db     *sql.DB
	handle *codec.MsgpackHandle
	log    *zap.Logger
	mu     sync.RWMutex
}

var openMu sync.Mutex

type encodedPayload struct {
	Data   any
	Format payload.Format
}

type encodedEntry struct {
	Meta attrs.Bag
	Data *encodedPayload
	ID   registry.ID
	Kind string
}

func newMsgpackHandle() *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{}
	mh.MapType = reflect.TypeOf(map[string]any(nil))
	mh.SliceType = nil
	mh.RawToString = true
	mh.Canonical = true
	mh.StructToArray = false
	return mh
}

func NewSQLite(dbPath string, log *zap.Logger) (*History, error) {
	openMu.Lock()
	defer openMu.Unlock()

	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, NewOpenDatabaseError(err)
		}
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", dbPath))
	if err != nil {
		return nil, NewOpenDatabaseError(err)
	}
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, NewConnectError(err)
	}

	manager, err := schemamanager.NewManager(historyschema.SQLiteBundle(), schemamanager.Target{
		Dialect:     schemamanager.DialectSQLite,
		DB:          db,
		LogicalName: historyschema.BundleName,
	})
	if err != nil {
		_ = db.Close()
		return nil, NewMigrationError(err)
	}
	if err := manager.Setup(ctx); err != nil {
		_ = db.Close()
		return nil, NewMigrationError(err)
	}
	if err := manager.Update(ctx); err != nil {
		_ = db.Close()
		return nil, NewMigrationError(err)
	}

	h := &History{
		db:     db,
		handle: newMsgpackHandle(),
		log:    log,
	}

	if err := h.ensureRootVersion(); err != nil {
		_ = db.Close()
		return nil, NewEnsureRootVersionError(err)
	}

	return h, nil
}

func (h *History) ensureRootVersion() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return NewBeginTransactionError(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO versions (id, parent_id) VALUES (0, NULL)"); err != nil {
		return NewInsertRootVersionError(err)
	}

	// Create an empty changeset for v0.
	emptyChangesetData := []byte{0x90} // MessagePack empty array
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO changesets (version_id, data) VALUES (0, ?)", emptyChangesetData); err != nil {
		return NewInsertChangesetError(err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO metadata (key, value) VALUES ('head', '0')"); err != nil {
		return NewSetInitialHeadError(err)
	}

	if err := tx.Commit(); err != nil {
		return NewCommitTransactionError(err)
	}

	return nil
}

func (h *History) Versions() ([]registry.Version, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.versions(context.Background())
}

func (h *History) versions(ctx context.Context) ([]registry.Version, error) {
	rows, err := h.db.QueryContext(ctx, "SELECT id, parent_id FROM versions ORDER BY id ASC")
	if err != nil {
		return nil, NewQueryVersionsError(err)
	}
	defer func() { _ = rows.Close() }()

	versionMap := make(map[uint]registry.Version)
	versionList := make([]registry.Version, 0, 10)

	for rows.Next() {
		var id uint
		var parentID sql.NullInt64

		if err := rows.Scan(&id, &parentID); err != nil {
			return nil, NewScanVersionError(err)
		}

		var v registry.Version
		if parentID.Valid {
			if parentID.Int64 < 0 {
				return nil, NewInvalidParentVersionError(parentID.Int64)
			}
			parent, ok := versionMap[uint(parentID.Int64)]
			if !ok {
				return nil, NewParentVersionNotFoundError(uint(parentID.Int64), id)
			}
			v = version.FromParent(parent, id)
		} else {
			v = version.New(id)
		}

		versionMap[id] = v
		versionList = append(versionList, v)
	}

	if err := rows.Err(); err != nil {
		return nil, NewIterateVersionsError(err)
	}

	return versionList, nil
}

func (h *History) Get(v registry.Version) (registry.ChangeSet, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := context.Background()
	var data []byte
	err := h.db.QueryRowContext(ctx, "SELECT data FROM changesets WHERE version_id = ?", v.ID()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewChangesetNotFoundError(v.ID())
	}
	if err != nil {
		return nil, NewQueryChangesetError(err)
	}

	return h.decodeChangeSet(data)
}

func (h *History) decodeChangeSet(data []byte) (registry.ChangeSet, error) {
	var encodedOps []struct {
		Entry         encodedEntry
		OriginalEntry *encodedEntry
		Kind          string
	}

	decoder := codec.NewDecoder(bytes.NewReader(data), h.handle)
	if err := decoder.Decode(&encodedOps); err != nil {
		return nil, NewDecodeChangesetError(err)
	}

	cs := make(registry.ChangeSet, len(encodedOps))
	for i, encOp := range encodedOps {
		entry := registry.Entry{
			ID:   encOp.Entry.ID,
			Kind: encOp.Entry.Kind,
			Meta: encOp.Entry.Meta,
		}

		if encOp.Entry.Data != nil {
			entry.Data = payload.NewPayload(encOp.Entry.Data.Data, encOp.Entry.Data.Format)
		}

		op := registry.Operation{
			Kind:  encOp.Kind,
			Entry: entry,
		}

		if encOp.OriginalEntry != nil {
			originalEntry := registry.Entry{
				ID:   encOp.OriginalEntry.ID,
				Kind: encOp.OriginalEntry.Kind,
				Meta: encOp.OriginalEntry.Meta,
			}

			if encOp.OriginalEntry.Data != nil {
				originalEntry.Data = payload.NewPayload(encOp.OriginalEntry.Data.Data, encOp.OriginalEntry.Data.Format)
			}

			op.OriginalEntry = &originalEntry
		}

		cs[i] = op
	}

	return cs, nil
}

// ReplayChanges streams one target lineage in a single query. This keeps boot
// replay at constant query count and bounded decoded-payload memory even when a
// project has accumulated a very long history.
func (h *History) ReplayChanges(target registry.Version, apply func(registry.ChangeSet) error) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ctx := context.Background()
	var exists int
	if err := h.db.QueryRowContext(ctx, "SELECT 1 FROM versions WHERE id = ?", target.ID()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NewChangesetNotFoundError(target.ID())
		}
		return NewQueryChangesetError(err)
	}
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE lineage(id, parent_id, depth) AS (
		SELECT id, parent_id, 0 FROM versions WHERE id = ?
		UNION ALL
		SELECT v.id, v.parent_id, lineage.depth + 1
		FROM versions v JOIN lineage ON v.id = lineage.parent_id
	)
	SELECT c.data FROM lineage JOIN changesets c ON c.version_id = lineage.id
	WHERE lineage.id <> 0 ORDER BY lineage.depth DESC`, target.ID())
	if err != nil {
		return NewQueryChangesetError(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return NewQueryChangesetError(err)
		}
		changes, err := h.decodeChangeSet(data)
		if err != nil {
			return err
		}
		if err := apply(changes); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return NewIterateVersionsError(err)
	}
	return nil
}

func (h *History) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
	return h.SaveWithDependencyResolution(v, cs, nil, head)
}

func (h *History) SaveWithDependencyResolution(v registry.Version, cs registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return NewBeginTransactionError(err)
	}
	defer func() { _ = tx.Rollback() }()

	var parentID sql.NullInt64
	if v.Previous() != nil {
		prevID := v.Previous().ID()
		const maxInt64 = uint(1<<63 - 1)
		if prevID > maxInt64 {
			return NewParentVersionIDTooLargeError(prevID)
		}
		parentID = sql.NullInt64{Int64: int64(prevID), Valid: true}
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO versions (id, parent_id) VALUES (?, ?)", v.ID(), parentID)
	if err != nil {
		return NewInsertVersionError(err)
	}

	encodedOps := make([]struct {
		Entry         encodedEntry
		OriginalEntry *encodedEntry
		Kind          string
	}, len(cs))

	for i, op := range cs {
		var encPayload *encodedPayload
		if op.Entry.Data != nil {
			encPayload = &encodedPayload{
				Format: op.Entry.Data.Format(),
				Data:   op.Entry.Data.Data(),
			}
		}

		var encOriginal *encodedEntry
		if op.OriginalEntry != nil {
			var encOrigPayload *encodedPayload
			if op.OriginalEntry.Data != nil {
				encOrigPayload = &encodedPayload{
					Format: op.OriginalEntry.Data.Format(),
					Data:   op.OriginalEntry.Data.Data(),
				}
			}

			encOriginal = &encodedEntry{
				ID:   op.OriginalEntry.ID,
				Kind: op.OriginalEntry.Kind,
				Meta: op.OriginalEntry.Meta,
				Data: encOrigPayload,
			}
		}

		encodedOps[i] = struct {
			Entry         encodedEntry
			OriginalEntry *encodedEntry
			Kind          string
		}{
			Kind: op.Kind,
			Entry: encodedEntry{
				ID:   op.Entry.ID,
				Kind: op.Entry.Kind,
				Meta: op.Entry.Meta,
				Data: encPayload,
			},
			OriginalEntry: encOriginal,
		}
	}

	var buf bytes.Buffer
	encoder := codec.NewEncoder(&buf, h.handle)
	if err := encoder.Encode(encodedOps); err != nil {
		return NewEncodeChangesetError(err)
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO changesets (version_id, data) VALUES (?, ?)", v.ID(), buf.Bytes())
	if err != nil {
		return NewInsertChangesetError(err)
	}
	if resolution != nil {
		canonical := resolution.Canonical()
		data, marshalErr := json.Marshal(canonical)
		if marshalErr != nil {
			return NewEncodeChangesetError(marshalErr)
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO resolution_graphs (digest, data) VALUES (?, ?)", canonical.Digest, data); err != nil {
			return NewInsertChangesetError(err)
		}
		if err = ensureResolutionGraph(ctx, tx, canonical.Digest); err != nil {
			return NewDecodeChangesetError(err)
		}
		if err = setVersionResolution(ctx, tx, v.ID(), canonical.Digest); err != nil {
			return NewInsertChangesetError(err)
		}
	} else if v.Previous() != nil {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO version_resolutions (version_id, resolution_digest)
			SELECT ?, resolution_digest FROM version_resolutions WHERE version_id = ?`, v.ID(), v.Previous().ID()); err != nil {
			return NewInsertChangesetError(err)
		}
	}

	if head {
		if v.Previous() == nil {
			return NewUpdateHeadError(errors.New("head updates require an expected parent version"))
		}
		result, updateErr := tx.ExecContext(ctx, "UPDATE metadata SET value = ? WHERE key = 'head' AND value = ?", v.ID(), v.Previous().ID())
		err = updateErr
		if err != nil {
			return NewUpdateHeadError(err)
		}
		updated, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return NewUpdateHeadError(rowsErr)
		}
		if updated != 1 {
			return NewUpdateHeadError(fmt.Errorf("history head changed: expected version %d", v.Previous().ID()))
		}
	}

	if err := tx.Commit(); err != nil {
		return NewCommitTransactionError(err)
	}

	return nil
}

func (h *History) GetDependencyResolution(v registry.Version) (*registry.DependencyResolution, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var digest string
	var data []byte
	err := h.db.QueryRowContext(context.Background(), `
		SELECT vr.resolution_digest, g.data
		FROM version_resolutions vr
		LEFT JOIN resolution_graphs g ON g.digest = vr.resolution_digest
		WHERE vr.version_id = ?`, v.ID()).Scan(&digest, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, registry.ErrDependencyResolutionNotFound
	}
	if err != nil {
		return nil, NewQueryChangesetError(err)
	}
	resolution, err := decodeResolutionGraph(digest, data)
	if err != nil {
		return nil, NewDecodeChangesetError(err)
	}
	return resolution, nil
}

func (h *History) CheckpointDependencyResolution(v registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	canonical := resolution.Canonical()
	data, err := json.Marshal(canonical)
	if err != nil {
		return NewEncodeChangesetError(err)
	}
	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return NewBeginTransactionError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO resolution_graphs (digest, data) VALUES (?, ?)", canonical.Digest, data); err != nil {
		return NewInsertChangesetError(err)
	}
	if err = ensureResolutionGraph(ctx, tx, canonical.Digest); err != nil {
		return NewDecodeChangesetError(err)
	}
	if err = setVersionResolution(ctx, tx, v.ID(), canonical.Digest); err != nil {
		return NewInsertChangesetError(err)
	}
	if err = tx.Commit(); err != nil {
		return NewCommitTransactionError(err)
	}
	return nil
}

func (h *History) Head() (registry.Version, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := context.Background()
	var headID uint
	err := h.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key = 'head'").Scan(&headID)
	if errors.Is(err, sql.ErrNoRows) {
		return version.New(0), nil
	}
	if err != nil {
		return nil, NewQueryHeadError(err)
	}

	versions, err := h.versions(ctx)
	if err != nil {
		return nil, NewGetVersionsError(err)
	}

	for _, v := range versions {
		if v.ID() == headID {
			return v, nil
		}
	}

	return nil, NewHeadVersionNotFoundError(headID)
}

func (h *History) SetHead(v registry.Version) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	result, err := h.db.ExecContext(ctx, `UPDATE metadata SET value = ?
		WHERE key = 'head' AND EXISTS (SELECT 1 FROM versions WHERE id = ?)`, v.ID(), v.ID())
	if err != nil {
		return NewSetHeadError(err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return NewSetHeadError(err)
	}
	if updated != 1 {
		return NewSetHeadError(fmt.Errorf("version %d does not exist", v.ID()))
	}

	return nil
}

func (h *History) CompareAndSetHead(expected, target registry.Version) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	ctx := context.Background()
	result, err := h.db.ExecContext(ctx, `UPDATE metadata SET value = ?
		WHERE key = 'head' AND value = ? AND EXISTS (SELECT 1 FROM versions WHERE id = ?)`,
		target.ID(), expected.ID(), target.ID())
	if err != nil {
		return NewSetHeadError(err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return NewSetHeadError(err)
	}
	if updated != 1 {
		return NewSetHeadError(fmt.Errorf("history head changed or target version %d does not exist", target.ID()))
	}
	return nil
}

func ensureResolutionGraph(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, digest string) error {
	var data []byte
	if err := q.QueryRowContext(ctx, "SELECT data FROM resolution_graphs WHERE digest = ?", digest).Scan(&data); err != nil {
		return fmt.Errorf("query stored dependency resolution %s: %w", digest, err)
	}
	_, err := decodeResolutionGraph(digest, data)
	return err
}

func decodeResolutionGraph(digest string, data []byte) (*registry.DependencyResolution, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("stored dependency resolution %s has no graph payload", digest)
	}
	var resolution registry.DependencyResolution
	if err := json.Unmarshal(data, &resolution); err != nil {
		return nil, err
	}
	if !resolution.Valid() {
		return nil, errors.New("stored dependency resolution digest mismatch")
	}
	canonical := resolution.Canonical()
	if canonical.Digest != digest {
		return nil, fmt.Errorf("stored dependency resolution key mismatch: row %s, payload %s", digest, canonical.Digest)
	}
	return canonical, nil
}

func setVersionResolution(ctx context.Context, tx *sql.Tx, versionID uint, digest string) error {
	result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO version_resolutions (version_id, resolution_digest) VALUES (?, ?)", versionID, digest)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 1 {
		return nil
	}
	var stored string
	if err := tx.QueryRowContext(ctx, "SELECT resolution_digest FROM version_resolutions WHERE version_id = ?", versionID).Scan(&stored); err != nil {
		return err
	}
	if stored != digest {
		return fmt.Errorf("version %d already references dependency resolution %s, refusing %s", versionID, stored, digest)
	}
	return nil
}

func (h *History) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.log.Debug("closing SQLite history", zap.Bool("db_initialized", h.db != nil))

	if h.db != nil {
		err := h.db.Close()
		if err != nil {
			h.log.Error("failed to close SQLite database", zap.Error(err))
			return NewCloseDatabaseError(err)
		}
		h.log.Debug("SQLite history closed successfully")
		return nil
	}

	h.log.Debug("SQLite history close skipped, database not initialized")
	return nil
}
