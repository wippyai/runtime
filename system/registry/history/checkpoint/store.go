// SPDX-License-Identifier: MPL-2.0

// Package checkpoint persists the registry's replay reducer as a history cache.
package checkpoint

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/wippyai/runtime/api/registry"
)

// FormatVersion identifies the replay-state cache encoding.
const FormatVersion = 1

// policy bounds the authoritative tail read between checkpoints.
var policy = struct {
	Versions int
	Bytes    int64
}{Versions: 256, Bytes: 8 << 20}

// Queryer is the database read capability used by replay.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store binds replay cache operations to validated history table names and codecs.
type Store struct {
	Parameter                         func(int) string
	Decode                            func([]byte) (registry.ChangeSet, error)
	EncodeState                       func(*registry.ReplayState) ([]byte, error)
	DecodeState                       func([]byte) (*registry.ReplayState, error)
	Versions, Changesets, Checkpoints string
}

type ancestor struct {
	chain  sql.NullString
	id     uint
	parent sql.NullInt64
	size   sql.NullInt64
	format sql.NullInt64
}

// lineage reads metadata only; changeset payloads remain in the database.
func (s Store) lineage(ctx context.Context, q Queryer, target uint) ([]ancestor, error) {
	query := `WITH RECURSIVE lineage(id,parent_id,lineage_digest) AS (
 SELECT id,parent_id,lineage_digest FROM ` + s.Versions + ` WHERE id = ` + s.Parameter(1) + `
 UNION
 SELECT v.id,v.parent_id,v.lineage_digest FROM ` + s.Versions + ` v JOIN lineage ON v.id = lineage.parent_id
 WHERE NOT EXISTS (SELECT 1 FROM ` + s.Checkpoints + ` p WHERE p.version_id = lineage.id AND p.format_version = ` + s.Parameter(2) + `)
 ) SELECT l.id,l.parent_id,l.lineage_digest,length(c.data),p.format_version
 FROM lineage l LEFT JOIN ` + s.Changesets + ` c ON c.version_id=l.id
 LEFT JOIN ` + s.Checkpoints + ` p ON p.version_id=l.id`
	rows, err := q.QueryContext(ctx, query, target, FormatVersion)
	if err != nil {
		return nil, fmt.Errorf("query replay ancestry: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[uint]ancestor)
	for rows.Next() {
		var a ancestor
		if err := rows.Scan(&a.id, &a.parent, &a.chain, &a.size, &a.format); err != nil {
			return nil, fmt.Errorf("scan replay ancestry: %w", err)
		}
		byID[a.id] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read replay ancestry: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(byID) == 0 {
		return nil, fmt.Errorf("version %d not found", target)
	}
	var path []ancestor
	seen := make(map[uint]bool)
	for id := target; ; {
		a, ok := byID[id]
		if !ok || seen[id] || (id != registry.RootVersion && (!a.parent.Valid || a.parent.Int64 < 0)) || (id == registry.RootVersion && a.parent.Valid) {
			return nil, fmt.Errorf("version %d lineage does not terminate at root", target)
		}
		if !a.size.Valid {
			return nil, fmt.Errorf("changeset not found for version %d", id)
		}
		seen[id] = true
		path = append(path, a)
		if id == registry.RootVersion || (a.format.Valid && a.format.Int64 == FormatVersion) {
			break
		}
		id = uint(a.parent.Int64)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}

func nearest(path []ancestor) (index int, incompatible bool) {
	index = -1
	for i := len(path) - 1; i >= 0; i-- {
		if !path[i].format.Valid {
			continue
		}
		if path[i].format.Int64 == FormatVersion {
			return i, incompatible
		}
		incompatible = true
	}
	return index, incompatible
}

func number(h hash.Hash, n uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	_, _ = h.Write(b[:])
}

func binding(path []ancestor) (string, error) {
	last := path[len(path)-1]
	chain, err := hex.DecodeString(last.chain.String)
	if err != nil || len(chain) != sha256.Size {
		return "", fmt.Errorf("version %d has invalid replay lineage digest", last.id)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("registry-replay-lineage-v1"))
	_, _ = h.Write(chain)
	number(h, uint64(last.id))
	if last.parent.Valid {
		number(h, uint64(last.parent.Int64))
	} else {
		number(h, ^uint64(0))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func digest(lineage string, data []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte("registry-replay-cache-v1"))
	number(h, FormatVersion)
	_, _ = h.Write([]byte(lineage))
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (s Store) readState(ctx context.Context, q Queryer, path []ancestor, index int) (*registry.ReplayState, error) {
	if index < 0 {
		return registry.NewReplayState(), nil
	}
	var data []byte
	var storedDigest, storedBinding string
	err := q.QueryRowContext(ctx, "SELECT digest,lineage_digest,data FROM "+s.Checkpoints+" WHERE version_id="+s.Parameter(1), path[index].id).Scan(&storedDigest, &storedBinding, &data)
	if err != nil {
		return nil, fmt.Errorf("read replay checkpoint: %w", err)
	}
	expected, err := binding(path[:index+1])
	if err != nil {
		return nil, err
	}
	if storedBinding != expected {
		return nil, fmt.Errorf("replay checkpoint %d lineage mismatch", path[index].id)
	}
	if storedDigest != digest(storedBinding, data) {
		return nil, fmt.Errorf("replay checkpoint %d digest mismatch", path[index].id)
	}
	state, err := s.DecodeState(data)
	if err != nil {
		return nil, fmt.Errorf("decode replay checkpoint %d: %w", path[index].id, err)
	}
	return state, nil
}

func (s Store) stateReducer(state *registry.ReplayState) func(*ancestor, []byte) error {
	transcoder := NewReplayTranscoder()
	return func(a *ancestor, data []byte) error {
		if a.id == registry.RootVersion {
			return nil
		}
		changes, err := s.Decode(data)
		if err != nil {
			return err
		}
		return state.ApplyWithTranscoder(changes, transcoder)
	}
}

func (s Store) replay(ctx context.Context, q Queryer, path []ancestor, index int, reduce func(*ancestor, []byte) error) error {
	start := index + 1
	versions := make([]ReplayVersion, len(path)-start)
	for i, a := range path[start:] {
		versions[i] = ReplayVersion{ID: a.id, Size: a.size.Int64}
	}
	for len(versions) > 0 {
		page, err := s.ReadPage(ctx, q, versions)
		if err != nil {
			return err
		}
		for i, data := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := reduce(&path[start+i], data); err != nil {
				return err
			}
			page[i] = nil
		}
		start += len(page)
		versions = versions[len(page):]
	}
	return nil
}

func (s Store) project(ctx context.Context, q Queryer, path []ancestor, index int) (*registry.ReplayState, error) {
	state, err := s.readState(ctx, q, path, index)
	if err != nil {
		return nil, err
	}
	if err := s.replay(ctx, q, path, index, s.stateReducer(state)); err != nil {
		return nil, err
	}
	return state, nil
}

func (s Store) Load(ctx context.Context, q Queryer, target uint) (*registry.ReplayState, error) {
	path, err := s.lineage(ctx, q, target)
	if err != nil {
		return nil, err
	}
	index, _ := nearest(path)
	return s.project(ctx, q, path, index)
}

// MaybeWrite participates in Save/head-CAS; it never commits independently.
func (s Store) MaybeWrite(ctx context.Context, tx *sql.Tx, target uint) error {
	path, err := s.lineage(ctx, tx, target)
	if err != nil {
		return err
	}
	index, incompatible := nearest(path)
	var bytes int64
	start := max(index+1, 1)
	for _, a := range path[start:] {
		bytes += a.size.Int64
	}
	if !incompatible && len(path)-start < policy.Versions && bytes < policy.Bytes {
		return nil
	}
	state, err := s.readState(ctx, tx, path, index)
	if err != nil {
		return err
	}
	previous := ""
	if index >= 0 {
		previous = path[index].chain.String
	}
	reduce := s.stateReducer(state)
	if err := s.replay(ctx, tx, path, index, func(a *ancestor, data []byte) error {
		h := sha256.New()
		_, _ = h.Write([]byte("registry-history-content-v1"))
		_, _ = h.Write([]byte(previous))
		number(h, uint64(a.id))
		if a.parent.Valid {
			number(h, uint64(a.parent.Int64))
		} else {
			number(h, ^uint64(0))
		}
		content := sha256.Sum256(data)
		_, _ = h.Write(content[:])
		previous = hex.EncodeToString(h.Sum(nil))
		a.chain = sql.NullString{String: previous, Valid: true}
		return reduce(a, data)
	}); err != nil {
		return err
	}
	last := path[len(path)-1]
	// #nosec G202 -- backend constructors supply fixed or validated, quoted table names.
	if _, err := tx.ExecContext(ctx, "UPDATE "+s.Versions+" SET lineage_digest="+s.Parameter(1)+" WHERE id="+s.Parameter(2), last.chain.String, last.id); err != nil {
		return err
	}
	data, err := s.EncodeState(state)
	if err != nil {
		return err
	}
	lineage, err := binding(path)
	if err != nil {
		return err
	}
	// #nosec G202 -- backend constructors supply fixed or validated, quoted table names.
	query := "INSERT INTO " + s.Checkpoints + " (version_id,format_version,digest,lineage_digest,data) VALUES (" + s.Parameter(1) + "," + s.Parameter(2) + "," + s.Parameter(3) + "," + s.Parameter(4) + "," + s.Parameter(5) + ") ON CONFLICT(version_id) DO UPDATE SET format_version=excluded.format_version,digest=excluded.digest,lineage_digest=excluded.lineage_digest,data=excluded.data"
	if _, err := tx.ExecContext(ctx, query, target, FormatVersion, digest(lineage, data), lineage, data); err != nil {
		return fmt.Errorf("write replay checkpoint: %w", err)
	}
	return nil
}
