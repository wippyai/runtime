// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func (s *Source) runSnapshot(ctx context.Context, conn *sql.Conn) error {
	tables, err := s.snapshotTables(ctx, conn)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if err := s.snapshotTable(ctx, conn, table); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) snapshotTables(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if s.tableAllowed(name) {
			tables = append(tables, name)
		}
	}
	return tables, rows.Err()
}

func (s *Source) snapshotTable(ctx context.Context, conn *sql.Conn, table string) error {
	cols := s.columnsFor(ctx, table)

	rows, err := conn.QueryContext(ctx, "SELECT * FROM "+quoteIdent(table)) //nolint:gosec // quoted identifier from sqlite_master; SQLite cannot bind table names as parameters
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	names, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		cols = columnsFromNames(names)
	}

	for rows.Next() {
		vals := make([]any, len(names))
		ptrs := make([]any, len(names))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		seq := s.seq.Add(1)
		s.subs.publish(ctx, config.Change{
			Source:   s.name,
			Op:       "snapshot",
			Table:    table,
			Relation: table,
			After:    mapRow(cols, vals),
			LSN:      strconv.FormatUint(seq, 10),
		})
	}
	return rows.Err()
}

func resolveColumns(ctx context.Context, db *sql.DB, table string) ([]columnInfo, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cols []columnInfo
	for rows.Next() {
		var cid, notnull, pk int
		var name, declType string
		var dflt any
		if err := rows.Scan(&cid, &name, &declType, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, columnInfo{name: name, text: textAffinity(declType)})
	}
	return cols, rows.Err()
}

func columnsFromNames(names []string) []columnInfo {
	cols := make([]columnInfo, len(names))
	for i, n := range names {
		cols[i] = columnInfo{name: n}
	}
	return cols
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
