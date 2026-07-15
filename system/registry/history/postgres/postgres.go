// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	getResolution        string
	getResolutionGraph   string
	getVersionResolution string
	getChangeset         string
	inheritResolution    string
	insertResolution     string
	setVersionResolution string
	insertChangeset      string
	insertRootChangeset  string
	insertRootVersion    string
	insertVersion        string
	queryHead            string
	queryHeadLineage     string
	queryVersionLineage  string
	queryVersions        string
	queryMaxVersionID    string
	setHead              string
	setInitialHead       string
	updateHeadCAS        string
	versionExists        string
	replayChanges        string
}

type encodedPayload struct {
	Data   any
	Format payload.Format
}

type encodedEntry struct {
	Meta           attrs.Bag
	Data           *encodedPayload
	ID             registry.ID
	Kind           string
	DependencyRoot bool
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
	resolutionGraphs := table("resolution_graphs")
	versionResolutions := table("version_resolutions")
	metadata := table("metadata")
	versionLineage := `WITH RECURSIVE lineage(id, parent_id) AS (
		SELECT id, parent_id FROM ` + versions + ` WHERE id = $1
		UNION
		SELECT v.id, v.parent_id
		FROM ` + versions + ` v JOIN lineage ON v.id = lineage.parent_id
	)
	SELECT id, parent_id FROM lineage`

	return postgresQueries{
		getResolution:        "SELECT vr.resolution_digest, g.data FROM " + versionResolutions + " vr LEFT JOIN " + resolutionGraphs + " g ON g.digest = vr.resolution_digest WHERE vr.version_id = $1",
		getResolutionGraph:   "SELECT data FROM " + resolutionGraphs + " WHERE digest = $1",
		getVersionResolution: "SELECT resolution_digest FROM " + versionResolutions + " WHERE version_id = $1",
		getChangeset:         "SELECT data FROM " + changesets + " WHERE version_id = $1",
		inheritResolution:    "INSERT INTO " + versionResolutions + " (version_id, resolution_digest) SELECT $1, resolution_digest FROM " + versionResolutions + " WHERE version_id = $2 ON CONFLICT(version_id) DO NOTHING",
		insertResolution:     "INSERT INTO " + resolutionGraphs + " (digest, data) VALUES ($1, $2) ON CONFLICT(digest) DO NOTHING",
		setVersionResolution: "INSERT INTO " + versionResolutions + " (version_id, resolution_digest) VALUES ($1, $2) ON CONFLICT(version_id) DO NOTHING",
		insertChangeset:      "INSERT INTO " + changesets + " (version_id, data) VALUES ($1, $2)",
		insertRootChangeset:  "INSERT INTO " + changesets + " (version_id, data) VALUES (0, $1) ON CONFLICT(version_id) DO NOTHING",
		insertRootVersion:    "INSERT INTO " + versions + " (id, parent_id) VALUES (0, NULL) ON CONFLICT(id) DO NOTHING",
		insertVersion:        "INSERT INTO " + versions + " (id, parent_id) VALUES ($1, $2)",
		queryHead:            "SELECT value FROM " + metadata + " WHERE key = 'head'",
		queryHeadLineage:     versionLineage,
		queryVersionLineage:  versionLineage,
		queryVersions:        "SELECT id, parent_id FROM " + versions + " ORDER BY id ASC",
		queryMaxVersionID:    "SELECT COALESCE(MAX(id), 0) FROM " + versions,
		setHead:              "UPDATE " + metadata + " SET value = $1 WHERE key = 'head' AND EXISTS (SELECT 1 FROM " + versions + " WHERE id = $2)",
		setInitialHead:       "INSERT INTO " + metadata + " (key, value) VALUES ('head', '0') ON CONFLICT(key) DO NOTHING",
		updateHeadCAS:        "UPDATE " + metadata + " SET value = $1 WHERE key = 'head' AND value = $2 AND EXISTS (SELECT 1 FROM " + versions + " WHERE id = $3)",
		versionExists:        "SELECT 1 FROM " + versions + " WHERE id = $1",
		replayChanges: `WITH RECURSIVE lineage(id, parent_id) AS (
			SELECT id, parent_id FROM ` + versions + ` WHERE id = $1
			UNION
			SELECT v.id, v.parent_id
			FROM ` + versions + ` v JOIN lineage ON v.id = lineage.parent_id
		)
		SELECT lineage.id, lineage.parent_id, c.data
		FROM lineage LEFT JOIN ` + changesets + ` c ON c.version_id = lineage.id`,
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

func (h *History) MaxVersionID() (uint, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var maxID uint
	if err := h.db.QueryRowContext(context.Background(), h.queries.queryMaxVersionID).Scan(&maxID); err != nil {
		return 0, NewGetVersionsError(err)
	}
	return maxID, nil
}

func (h *History) GetVersion(id uint) (registry.Version, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	rows, err := h.db.QueryContext(context.Background(), h.queries.queryVersionLineage, id)
	if err != nil {
		return nil, NewQueryVersionsError(err)
	}
	parents := make(map[uint]sql.NullInt64)
	for rows.Next() {
		var versionID uint
		var parentID sql.NullInt64
		if err := rows.Scan(&versionID, &parentID); err != nil {
			_ = rows.Close()
			return nil, NewScanVersionError(err)
		}
		parents[versionID] = parentID
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, NewIterateVersionsError(err)
	}
	if err := rows.Close(); err != nil {
		return nil, NewIterateVersionsError(err)
	}
	stored, err := versionFromLineage(id, parents)
	if err != nil {
		return nil, NewQueryVersionsError(err)
	}
	return stored, nil
}

func versionFromLineage(targetID uint, parents map[uint]sql.NullInt64) (registry.Version, error) {
	ids := make([]uint, 0, len(parents))
	seen := make(map[uint]struct{}, len(parents))
	currentID := targetID
	for currentID != registry.RootVersion {
		if _, duplicate := seen[currentID]; duplicate {
			return nil, fmt.Errorf("version %d lineage contains a cycle", targetID)
		}
		seen[currentID] = struct{}{}
		parentID, ok := parents[currentID]
		if !ok {
			return nil, fmt.Errorf("version %d not found", currentID)
		}
		if !parentID.Valid || parentID.Int64 < 0 {
			return nil, fmt.Errorf("version %d lineage does not terminate at root", targetID)
		}
		ids = append(ids, currentID)
		currentID = uint(parentID.Int64)
	}
	rootParent, ok := parents[registry.RootVersion]
	if !ok || rootParent.Valid {
		return nil, fmt.Errorf("version %d lineage does not terminate at root", targetID)
	}
	current := version.New(registry.RootVersion)
	for i := len(ids) - 1; i >= 0; i-- {
		current = version.FromParent(current, ids[i])
	}
	return current, nil
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
		} else if id == registry.RootVersion {
			v = version.New(id)
		} else {
			return nil, NewParentVersionNotFoundError(registry.RootVersion, id)
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

	return h.decodeChangeSet(data)
}

func (h *History) decodeChangeSet(data []byte) (registry.ChangeSet, error) {
	var encodedOps []struct {
		OriginalEntry *encodedEntry
		Kind          string
		Entry         encodedEntry
	}

	decoder := codec.NewDecoder(bytes.NewReader(data), h.handle)
	if err := decoder.Decode(&encodedOps); err != nil {
		return nil, NewDecodeChangesetError(err)
	}

	cs := make(registry.ChangeSet, len(encodedOps))
	for i, encOp := range encodedOps {
		entry := registry.Entry{
			ID:             encOp.Entry.ID,
			Kind:           encOp.Entry.Kind,
			Meta:           encOp.Entry.Meta,
			DependencyRoot: encOp.Entry.DependencyRoot,
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
				ID:             encOp.OriginalEntry.ID,
				Kind:           encOp.OriginalEntry.Kind,
				Meta:           encOp.OriginalEntry.Meta,
				DependencyRoot: encOp.OriginalEntry.DependencyRoot,
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

func (h *History) ReplayChanges(ctx context.Context, target registry.Version, apply func(registry.ChangeSet) error) error {
	h.mu.RLock()
	encoded, err := h.replayLineage(ctx, target)
	h.mu.RUnlock()
	if err != nil {
		return err
	}
	for _, item := range encoded {
		if err := ctx.Err(); err != nil {
			return err
		}
		changes, err := h.decodeChangeSet(item)
		if err != nil {
			return err
		}
		if err := apply(changes); err != nil {
			return err
		}
	}
	return nil
}

type replayLineageRow struct {
	data     []byte
	parentID sql.NullInt64
}

func (h *History) replayLineage(ctx context.Context, target registry.Version) ([][]byte, error) {
	var exists int
	if err := h.db.QueryRowContext(ctx, h.queries.versionExists, target.ID()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NewChangesetNotFoundError(target.ID())
		}
		return nil, NewQueryChangesetError(err)
	}
	rows, err := h.db.QueryContext(ctx, h.queries.replayChanges, target.ID())
	if err != nil {
		return nil, NewQueryChangesetError(err)
	}
	lineage := make(map[uint]replayLineageRow)
	for rows.Next() {
		var versionID uint
		var parentID sql.NullInt64
		var data []byte
		if err := rows.Scan(&versionID, &parentID, &data); err != nil {
			_ = rows.Close()
			return nil, NewQueryChangesetError(err)
		}
		lineage[versionID] = replayLineageRow{parentID: parentID, data: data}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, NewIterateVersionsError(err)
	}
	if err := rows.Close(); err != nil {
		return nil, NewIterateVersionsError(err)
	}

	encoded := make([][]byte, 0, len(lineage))
	seen := make(map[uint]struct{}, len(lineage))
	currentID := target.ID()
	for currentID != registry.RootVersion {
		if _, duplicate := seen[currentID]; duplicate {
			return nil, NewQueryChangesetError(fmt.Errorf("version %d lineage is cyclic", target.ID()))
		}
		seen[currentID] = struct{}{}
		row, ok := lineage[currentID]
		if !ok || !row.parentID.Valid || row.parentID.Int64 < 0 {
			return nil, NewQueryChangesetError(fmt.Errorf("version %d lineage does not terminate at root", target.ID()))
		}
		if row.data == nil {
			return nil, NewChangesetNotFoundError(currentID)
		}
		encoded = append(encoded, row.data)
		currentID = uint(row.parentID.Int64)
	}
	root, ok := lineage[registry.RootVersion]
	if !ok || root.parentID.Valid {
		return nil, NewQueryChangesetError(fmt.Errorf("version %d lineage does not terminate at root", target.ID()))
	}
	for left, right := 0, len(encoded)-1; left < right; left, right = left+1, right-1 {
		encoded[left], encoded[right] = encoded[right], encoded[left]
	}
	return encoded, nil
}

func (h *History) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
	return h.SaveWithDependencyResolution(v, cs, nil, head)
}

func (h *History) SaveWithDependencyResolution(v registry.Version, cs registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	if v.ID() != registry.RootVersion && v.Previous() == nil {
		return NewInsertVersionError(fmt.Errorf("non-root version %d has no parent", v.ID()))
	}
	var canonicalResolution *registry.DependencyResolution
	if resolution != nil {
		canonicalResolution = resolution.Canonical()
		if !canonicalResolution.Valid() {
			return registry.ErrInvalidDependencyResolution
		}
	}
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
		const maxInt64 = uint64(1<<63 - 1)
		if uint64(prevID) > maxInt64 {
			return NewParentVersionIDTooLargeError(prevID)
		}
		parentID = sql.NullInt64{Int64: int64(prevID), Valid: true}
	}

	_, err = tx.ExecContext(ctx, h.queries.insertVersion, v.ID(), parentID)
	if err != nil {
		return NewInsertVersionError(err)
	}

	encodedOps := make([]struct {
		OriginalEntry *encodedEntry
		Kind          string
		Entry         encodedEntry
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
				ID:             op.OriginalEntry.ID,
				Kind:           op.OriginalEntry.Kind,
				Meta:           op.OriginalEntry.Meta,
				Data:           encOrigPayload,
				DependencyRoot: op.OriginalEntry.DependencyRoot,
			}
		}

		encodedOps[i] = struct {
			OriginalEntry *encodedEntry
			Kind          string
			Entry         encodedEntry
		}{
			Kind: op.Kind,
			Entry: encodedEntry{
				ID:             op.Entry.ID,
				Kind:           op.Entry.Kind,
				Meta:           op.Entry.Meta,
				Data:           encPayload,
				DependencyRoot: op.Entry.DependencyRoot,
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
	if canonicalResolution != nil {
		data, marshalErr := json.Marshal(canonicalResolution)
		if marshalErr != nil {
			return NewEncodeChangesetError(marshalErr)
		}
		if _, err = tx.ExecContext(ctx, h.queries.insertResolution, canonicalResolution.Digest, data); err != nil {
			return NewInsertChangesetError(err)
		}
		if err = h.ensureResolutionGraph(ctx, tx, canonicalResolution.Digest); err != nil {
			return NewDecodeChangesetError(err)
		}
		if err = h.setVersionResolution(ctx, tx, v.ID(), canonicalResolution.Digest); err != nil {
			return NewInsertChangesetError(err)
		}
	} else if v.Previous() != nil {
		if _, err = tx.ExecContext(ctx, h.queries.inheritResolution, v.ID(), v.Previous().ID()); err != nil {
			return NewInsertChangesetError(err)
		}
	}

	if head {
		if v.Previous() == nil {
			return NewUpdateHeadError(errors.New("head updates require an expected parent version"))
		}
		result, updateErr := tx.ExecContext(ctx, h.queries.updateHeadCAS,
			strconv.FormatUint(uint64(v.ID()), 10), strconv.FormatUint(uint64(v.Previous().ID()), 10), v.ID())
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
	err := h.db.QueryRowContext(context.Background(), h.queries.getResolution, v.ID()).Scan(&digest, &data)
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
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return registry.ErrInvalidDependencyResolution
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := json.Marshal(canonical)
	if err != nil {
		return NewEncodeChangesetError(err)
	}
	tx, err := h.db.BeginTx(context.Background(), nil)
	if err != nil {
		return NewBeginTransactionError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(context.Background(), h.queries.insertResolution, canonical.Digest, data); err != nil {
		return NewInsertChangesetError(err)
	}
	if err = h.ensureResolutionGraph(context.Background(), tx, canonical.Digest); err != nil {
		return NewDecodeChangesetError(err)
	}
	if err = h.setVersionResolution(context.Background(), tx, v.ID(), canonical.Digest); err != nil {
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

	rows, err := h.db.QueryContext(ctx, h.queries.queryHeadLineage, headID)
	if err != nil {
		return nil, NewQueryHeadError(err)
	}
	defer func() { _ = rows.Close() }()
	parents := make(map[uint]sql.NullInt64)
	for rows.Next() {
		var id uint
		var parentID sql.NullInt64
		if err := rows.Scan(&id, &parentID); err != nil {
			return nil, NewQueryHeadError(err)
		}
		parents[id] = parentID
	}
	if err := rows.Err(); err != nil {
		return nil, NewQueryHeadError(err)
	}
	stored, err := versionFromLineage(headID, parents)
	if err != nil {
		return nil, NewHeadVersionNotFoundError(headID)
	}
	return stored, nil
}

func (h *History) SetHead(v registry.Version) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx := context.Background()
	result, err := h.db.ExecContext(ctx, h.queries.setHead, strconv.FormatUint(uint64(v.ID()), 10), v.ID())
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
	result, err := h.db.ExecContext(ctx, h.queries.updateHeadCAS,
		strconv.FormatUint(uint64(target.ID()), 10), strconv.FormatUint(uint64(expected.ID()), 10), target.ID())
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

func (h *History) CompareAndSetHeadWithDependencyResolution(expected, target registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return registry.ErrInvalidDependencyResolution
	}
	h.mu.Lock()
	defer h.mu.Unlock()
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
	if _, err = tx.ExecContext(ctx, h.queries.insertResolution, canonical.Digest, data); err != nil {
		return NewInsertChangesetError(err)
	}
	if err = h.ensureResolutionGraph(ctx, tx, canonical.Digest); err != nil {
		return NewDecodeChangesetError(err)
	}
	if err = h.setVersionResolution(ctx, tx, target.ID(), canonical.Digest); err != nil {
		return NewInsertChangesetError(err)
	}
	result, err := tx.ExecContext(ctx, h.queries.updateHeadCAS,
		strconv.FormatUint(uint64(target.ID()), 10), strconv.FormatUint(uint64(expected.ID()), 10), target.ID())
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
	if err = tx.Commit(); err != nil {
		return NewCommitTransactionError(err)
	}
	return nil
}

func (h *History) ensureResolutionGraph(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, digest string) error {
	var data []byte
	if err := q.QueryRowContext(ctx, h.queries.getResolutionGraph, digest).Scan(&data); err != nil {
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

func (h *History) setVersionResolution(ctx context.Context, tx *sql.Tx, versionID uint, digest string) error {
	result, err := tx.ExecContext(ctx, h.queries.setVersionResolution, versionID, digest)
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
	if err := tx.QueryRowContext(ctx, h.queries.getVersionResolution, versionID).Scan(&stored); err != nil {
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
