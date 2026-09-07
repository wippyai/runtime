// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

func (s *sqliteConnectionState) finalRowsMatch(changes []capturedMutation) (bool, error) {
	if len(changes) == 0 {
		return true, nil
	}
	idsByTable := make(map[string][]int64)
	seen := make(map[string]map[int64]struct{})
	for _, change := range changes {
		rowID := change.RowID
		if change.Op == "delete" {
			rowID = change.OldRowID
		}
		tableKey := change.Schema + "\x00" + change.Table
		if seen[tableKey] == nil {
			seen[tableKey] = make(map[int64]struct{})
		}
		if _, ok := seen[tableKey][rowID]; !ok {
			seen[tableKey][rowID] = struct{}{}
			idsByTable[tableKey] = append(idsByTable[tableKey], rowID)
		}
	}

	rowsByKey := make(map[mutationKey][]any)
	for tableKey, rowIDs := range idsByTable {
		parts := strings.SplitN(tableKey, "\x00", 2)
		if len(parts) != 2 {
			return false, errors.New("sqlite mutation observer table key is invalid")
		}
		columns, err := s.tableColumns(parts[0], parts[1])
		if err != nil {
			return false, err
		}
		for start := 0; start < len(rowIDs); start += 500 {
			end := start + 500
			if end > len(rowIDs) {
				end = len(rowIDs)
			}
			placeholders := make([]string, end-start)
			args := make([]driver.Value, end-start)
			for i, rowID := range rowIDs[start:end] {
				placeholders[i] = "?"
				args[i] = rowID
			}
			query := fmt.Sprintf("SELECT rowid, %s FROM %s.%s WHERE rowid IN (%s)", storageProjection(columns), quoteIdentifier(parts[0]), quoteIdentifier(parts[1]), strings.Join(placeholders, ","))
			rawRows, err := s.sqlite.Query(query, args)
			if err != nil {
				return false, err
			}
			values := make([]driver.Value, len(rawRows.Columns()))
			for {
				err = rawRows.Next(values)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					_ = rawRows.Close()
					return false, err
				}
				rowID, ok := values[0].(int64)
				if !ok {
					_ = rawRows.Close()
					return false, fmt.Errorf("sqlite mutation observer returned rowid type %T", values[0])
				}
				after := make([]any, len(values)-1)
				for i := range after {
					after[i] = values[i+1]
				}
				rowsByKey[mutationKey{schema: parts[0], table: parts[1], rowID: rowID}] = after
			}
			_ = rawRows.Close()
		}
	}

	for _, change := range changes {
		rowID := change.RowID
		if change.Op == "delete" {
			rowID = change.OldRowID
		}
		row, exists := rowsByKey[mutationKey{schema: change.Schema, table: change.Table, rowID: rowID}]
		switch change.Op {
		case "delete":
			if exists {
				return false, nil
			}
		case "insert", "update":
			if !exists || !mutationValuesEqual(row, change.After) {
				return false, nil
			}
		default:
			return false, fmt.Errorf("sqlite mutation observer cannot verify operation %q", change.Op)
		}
	}
	return true, nil
}

func mutationValuesEqual(left, right []any) bool {
	// Both paths now preserve storage types. Equal bytes in TEXT and BLOB do
	// not prove the same stored row, so reconciliation must not coerce them.
	return reflect.DeepEqual(left, right)
}
