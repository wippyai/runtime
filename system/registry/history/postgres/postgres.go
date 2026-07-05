// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	_ "github.com/lib/pq" // Register PostgreSQL database driver
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historyschema "github.com/wippyai/runtime/system/registry/history/schema"
	schemamanager "github.com/wippyai/runtime/system/schema"
	"go.uber.org/zap"
)

const defaultSchema = "wippy_registry"

type History struct {
	db      *sql.DB
	handle  *codec.MsgpackHandle
	log     *zap.Logger
	queries postgresQueries
	mu      sync.RWMutex
}

type postgresQueries struct {
	getChangeset        string
	insertChangeset     string
	insertRootChangeset string
	insertRootVersion   string
	insertVersion       string
	queryHead           string
	queryVersions       string
	setHead             string
	setInitialHead      string
	updateHead          string
}

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

func NewPostgres(dsn string, schemaName string, log *zap.Logger) (*History, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, NewInvalidConfigError("history DSN is required")
	}
	schemaName = strings.TrimSpace(schemaName)
	if schemaName == "" {
		schemaName = defaultSchema
	}
	if err := schemamanager.ValidateIdentifier(schemaName); err != nil {
		return nil, NewInvalidConfigError(err.Error())
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, NewOpenDatabaseError(err)
	}

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, NewConnectError(err)
	}

	manager, err := schemamanager.NewManager(historyschema.PostgresBundle(), schemamanager.Target{
		Dialect:     schemamanager.DialectPostgres,
		DB:          db,
		LogicalName: historyschema.BundleName,
		SchemaName:  schemaName,
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
		db:      db,
		handle:  newMsgpackHandle(),
		log:     log,
		queries: buildQueries(schemaName),
	}

	if err := h.ensureRootVersion(); err != nil {
		_ = db.Close()
		return nil, NewEnsureRootVersionError(err)
	}

	return h, nil
}

func buildQueries(schemaName string) postgresQueries {
	table := func(name string) string {
		return schemamanager.QuoteIdentifier(schemaName) + "." + schemamanager.QuoteIdentifier(name)
	}
	versions := table("versions")
	changesets := table("changesets")
	metadata := table("metadata")

	return postgresQueries{
		getChangeset:        "SELECT data FROM " + changesets + " WHERE version_id = $1",
		insertChangeset:     "INSERT INTO " + changesets + " (version_id, data) VALUES ($1, $2) ON CONFLICT(version_id) DO UPDATE SET data = EXCLUDED.data",
		insertRootChangeset: "INSERT INTO " + changesets + " (version_id, data) VALUES (0, $1) ON CONFLICT(version_id) DO NOTHING",
		insertRootVersion:   "INSERT INTO " + versions + " (id, parent_id) VALUES (0, NULL) ON CONFLICT(id) DO NOTHING",
		insertVersion:       "INSERT INTO " + versions + " (id, parent_id) VALUES ($1, $2) ON CONFLICT(id) DO UPDATE SET parent_id = EXCLUDED.parent_id",
		queryHead:           "SELECT value FROM " + metadata + " WHERE key = 'head'",
		queryVersions:       "SELECT id, parent_id FROM " + versions + " ORDER BY id ASC",
		setHead:             "INSERT INTO " + metadata + " (key, value) VALUES ('head', $1) ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value",
		setInitialHead:      "INSERT INTO " + metadata + " (key, value) VALUES ('head', '0') ON CONFLICT(key) DO NOTHING",
		updateHead:          "INSERT INTO " + metadata + " (key, value) VALUES ('head', $1) ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value",
	}
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

	if _, err := tx.ExecContext(ctx, h.queries.insertRootVersion); err != nil {
		return NewInsertRootVersionError(err)
	}

	emptyChangesetData := []byte{0x90}
	if _, err := tx.ExecContext(ctx, h.queries.insertRootChangeset, emptyChangesetData); err != nil {
		return NewInsertChangesetError(err)
	}

	if _, err := tx.ExecContext(ctx, h.queries.setInitialHead); err != nil {
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
	rows, err := h.db.QueryContext(ctx, h.queries.queryVersions)
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
	err := h.db.QueryRowContext(ctx, h.queries.getChangeset, v.ID()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewChangesetNotFoundError(v.ID())
	}
	if err != nil {
		return nil, NewQueryChangesetError(err)
	}

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

func (h *History) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
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

	_, err = tx.ExecContext(ctx, h.queries.insertVersion, v.ID(), parentID)
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

	_, err = tx.ExecContext(ctx, h.queries.insertChangeset, v.ID(), buf.Bytes())
	if err != nil {
		return NewInsertChangesetError(err)
	}

	if head {
		_, err = tx.ExecContext(ctx, h.queries.updateHead, strconv.FormatUint(uint64(v.ID()), 10))
		if err != nil {
			return NewUpdateHeadError(err)
		}
	}

	if err := tx.Commit(); err != nil {
		return NewCommitTransactionError(err)
	}

	return nil
}

func (h *History) Head() (registry.Version, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx := context.Background()
	var headValue string
	err := h.db.QueryRowContext(ctx, h.queries.queryHead).Scan(&headValue)
	if errors.Is(err, sql.ErrNoRows) {
		return version.New(0), nil
	}
	if err != nil {
		return nil, NewQueryHeadError(err)
	}
	head64, err := strconv.ParseUint(headValue, 10, 0)
	if err != nil {
		return nil, NewParseHeadError(headValue, err)
	}
	headID := uint(head64)

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
	_, err := h.db.ExecContext(ctx, h.queries.setHead, strconv.FormatUint(uint64(v.ID()), 10))
	if err != nil {
		return NewSetHeadError(err)
	}

	return nil
}

func (h *History) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.log.Debug("closing PostgreSQL history", zap.Bool("db_initialized", h.db != nil))

	if h.db != nil {
		err := h.db.Close()
		if err != nil {
			h.log.Error("failed to close PostgreSQL database", zap.Error(err))
			return NewCloseDatabaseError(err)
		}
		h.log.Debug("PostgreSQL history closed successfully")
		return nil
	}

	h.log.Debug("PostgreSQL history close skipped, database not initialized")
	return nil
}
