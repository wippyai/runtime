// SPDX-License-Identifier: MPL-2.0

package checkpoint

import (
	"context"
	"fmt"
	"strings"
)

// replayPagePolicy bounds encoded payloads retained while the database cursor
// is closed for callbacks. A changeset larger than Bytes occupies a page alone.
var replayPagePolicy = struct {
	Rows  int
	Bytes int64
}{Rows: 1024, Bytes: 64 << 20}

// ReplayVersion identifies one member of a validated ancestry and its wire size.
type ReplayVersion struct {
	ID   uint
	Size int64
}

// MissingChangesetError identifies a missing authoritative payload after ancestry validation.
type MissingChangesetError struct {
	VersionID uint
}

func (e *MissingChangesetError) Error() string {
	return fmt.Sprintf("changeset not found for version %d", e.VersionID)
}

// ReadPage reads a bounded prefix of path. It closes the cursor before returning
// encoded changesets in ancestry order, including when version IDs are unordered.
func (s Store) ReadPage(ctx context.Context, q Queryer, path []ReplayVersion) ([][]byte, error) {
	if len(path) == 0 {
		return nil, nil
	}
	var bytes int64
	count := 0
	for _, v := range path {
		if count == replayPagePolicy.Rows || (count > 0 && v.Size > replayPagePolicy.Bytes-bytes) {
			break
		}
		bytes += v.Size
		count++
	}
	path = path[:count]
	args := make([]any, 2, count+2)
	positions := make(map[uint]int, count)
	low, high := path[0].ID, path[0].ID
	for i, v := range path {
		positions[v.ID] = i
		low, high = min(low, v.ID), max(high, v.ID)
	}
	// The range uses the changeset primary key. Sparse/branched lineages also
	// constrain membership so unrelated payloads never consume the page budget.
	// Bound parameters retain PostgreSQL binary blob transfer.
	args[0], args[1] = low, high
	query := "SELECT version_id,data FROM " + s.Changesets + " WHERE version_id BETWEEN " + s.Parameter(1) + " AND " + s.Parameter(2)
	if high-low != uint(count-1) {
		parameters := make([]string, count)
		for i, v := range path {
			parameters[i] = s.Parameter(i + 3)
			args = append(args, v.ID)
		}
		query += " AND version_id IN (" + strings.Join(parameters, ",") + ")"
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query replay page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	data := make([][]byte, count)
	for rows.Next() {
		var id uint
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, fmt.Errorf("scan replay page: %w", err)
		}
		position, ok := positions[id]
		if !ok || data[position] != nil {
			return nil, fmt.Errorf("unexpected replay changeset %d", id)
		}
		if int64(len(payload)) != path[position].Size {
			return nil, fmt.Errorf("replay changeset %d size changed", id)
		}
		data[position] = payload
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read replay page: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i, payload := range data {
		if payload == nil {
			return nil, &MissingChangesetError{VersionID: path[i].ID}
		}
	}
	return data, nil
}
