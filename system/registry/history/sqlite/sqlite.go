package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	_ "github.com/mattn/go-sqlite3" // Register SQLite3 database driver
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

type SQLiteHistory struct {
	db     *sql.DB
	mu     sync.RWMutex
	handle *codec.MsgpackHandle
	log    *zap.Logger
}

type encodedPayload struct {
	Format payload.Format
	Data   any
}

type encodedEntry struct {
	ID   registry.ID
	Kind string
	Meta registry.Metadata
	Data *encodedPayload
}

func newMsgpackHandle() *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{}
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	mh.SliceType = nil
	mh.RawToString = true
	mh.Canonical = true
	mh.StructToArray = false
	return mh
}

func NewSQLite(dbPath string, log *zap.Logger) (*SQLiteHistory, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	h := &SQLiteHistory{
		db:     db,
		handle: newMsgpackHandle(),
		log:    log,
	}

	if err := h.ensureRootVersion(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ensure root version: %w", err)
	}

	return h, nil
}

func (h *SQLiteHistory) ensureRootVersion() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	var exists bool
	err := h.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM versions WHERE id = 0)").Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check root version: %w", err)
	}

	if !exists {
		_, err := h.db.ExecContext(ctx, "INSERT INTO versions (id, parent_id) VALUES (0, NULL)")
		if err != nil {
			return fmt.Errorf("failed to insert root version: %w", err)
		}

		// Create an empty changeset for v0
		emptyChangesetData := []byte{0x90} // MessagePack empty array
		_, err = h.db.ExecContext(ctx, "INSERT INTO changesets (version_id, data) VALUES (0, ?)", emptyChangesetData)
		if err != nil {
			return fmt.Errorf("failed to insert empty changeset for v0: %w", err)
		}

		_, err = h.db.ExecContext(ctx, "INSERT OR REPLACE INTO metadata (key, value) VALUES ('head', '0')")
		if err != nil {
			return fmt.Errorf("failed to set initial head: %w", err)
		}
	}

	return nil
}

func (h *SQLiteHistory) Versions() ([]registry.Version, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := context.Background()
	rows, err := h.db.QueryContext(ctx, "SELECT id, parent_id FROM versions ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query versions: %w", err)
	}
	defer rows.Close()

	versionMap := make(map[uint]registry.Version)
	versionList := make([]registry.Version, 0, 10)

	for rows.Next() {
		var id uint
		var parentID sql.NullInt64

		if err := rows.Scan(&id, &parentID); err != nil {
			return nil, fmt.Errorf("failed to scan version: %w", err)
		}

		var v registry.Version
		if parentID.Valid {
			parent, ok := versionMap[uint(parentID.Int64)]
			if !ok {
				return nil, fmt.Errorf("parent version %d not found for version %d", parentID.Int64, id)
			}
			v = version.FromParent(parent, id)
		} else {
			v = version.New(id)
		}

		versionMap[id] = v
		versionList = append(versionList, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating versions: %w", err)
	}

	return versionList, nil
}

func (h *SQLiteHistory) Get(v registry.Version) (registry.ChangeSet, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := context.Background()
	var data []byte
	err := h.db.QueryRowContext(ctx, "SELECT data FROM changesets WHERE version_id = ?", v.ID()).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("changeset not found for version %d", v.ID())
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query changeset: %w", err)
	}

	var encodedOps []struct {
		Kind          string
		Entry         encodedEntry
		OriginalEntry *encodedEntry
	}

	decoder := codec.NewDecoder(bytes.NewReader(data), h.handle)
	if err := decoder.Decode(&encodedOps); err != nil {
		return nil, fmt.Errorf("failed to decode changeset: %w", err)
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

func (h *SQLiteHistory) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var parentID sql.NullInt64
	if v.Previous() != nil {
		parentID = sql.NullInt64{Int64: int64(v.Previous().ID()), Valid: true}
	}

	_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO versions (id, parent_id) VALUES (?, ?)", v.ID(), parentID)
	if err != nil {
		return fmt.Errorf("failed to insert version: %w", err)
	}

	encodedOps := make([]struct {
		Kind          string
		Entry         encodedEntry
		OriginalEntry *encodedEntry
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
			Kind          string
			Entry         encodedEntry
			OriginalEntry *encodedEntry
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
		return fmt.Errorf("failed to encode changeset: %w", err)
	}

	_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO changesets (version_id, data) VALUES (?, ?)", v.ID(), buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to insert changeset: %w", err)
	}

	if head {
		_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO metadata (key, value) VALUES ('head', ?)", v.ID())
		if err != nil {
			return fmt.Errorf("failed to update head: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (h *SQLiteHistory) Head() (registry.Version, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := context.Background()
	var headID uint
	err := h.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key = 'head'").Scan(&headID)
	if errors.Is(err, sql.ErrNoRows) {
		return version.New(0), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query head: %w", err)
	}

	versions, err := h.Versions()
	if err != nil {
		return nil, fmt.Errorf("failed to get versions: %w", err)
	}

	for _, v := range versions {
		if v.ID() == headID {
			return v, nil
		}
	}

	return nil, fmt.Errorf("head version %d not found", headID)
}

func (h *SQLiteHistory) SetHead(v registry.Version) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	_, err := h.db.ExecContext(ctx, "INSERT OR REPLACE INTO metadata (key, value) VALUES ('head', ?)", v.ID())
	if err != nil {
		return fmt.Errorf("failed to set head: %w", err)
	}

	return nil
}

func (h *SQLiteHistory) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.log.Debug("closing SQLite history", zap.Bool("db_initialized", h.db != nil))

	if h.db != nil {
		err := h.db.Close()
		if err != nil {
			h.log.Error("failed to close SQLite database", zap.Error(err))
			return fmt.Errorf("failed to close database: %w", err)
		}
		h.log.Debug("SQLite history closed successfully")
		return nil
	}

	h.log.Debug("SQLite history close skipped, database not initialized")
	return nil
}
