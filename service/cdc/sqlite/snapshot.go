// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"strings"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func (s *Source) bootstrapSubscription(ctx context.Context, sub *subscription) {
	defer sub.finishSnapshot()

	snapDB, err := openSnapshotConn(s.file)
	if err != nil {
		sub.fail("snapshot open: " + err.Error())
		return
	}
	defer func() { _ = snapDB.Close() }()

	tx, err := s.fenceAndBegin(ctx, snapDB)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		sub.fail("snapshot begin: " + err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()

	tables, err := s.snapshotTables(ctx, tx, sub)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		sub.fail("snapshot tables: " + err.Error())
		return
	}

	for _, table := range tables {
		if err := s.streamSnapshotTable(ctx, tx, sub, table); err != nil {
			if ctx.Err() != nil || sub.isClosed() {
				return
			}
			sub.fail("snapshot table " + table + ": " + err.Error())
			return
		}
	}
}

func (s *Source) fenceAndBegin(ctx context.Context, snapDB *sql.DB) (*sql.Tx, error) {
	s.mu.Lock()
	writerDB := s.writerDB
	s.mu.Unlock()
	if writerDB == nil {
		return nil, ErrSourceClosed
	}

	wc, err := writerDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = wc.Close() }()

	if err := wc.PingContext(ctx); err != nil {
		return nil, err
	}

	tx, err := snapDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}

	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&count); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	return tx, nil
}

func (s *Source) snapshotTables(ctx context.Context, tx *sql.Tx, sub *subscription) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
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
		if s.tableAllowed(name) && sub.tableAllowed(name) {
			tables = append(tables, name)
		}
	}

	return tables, rows.Err()
}

func (s *Source) streamSnapshotTable(ctx context.Context, tx *sql.Tx, sub *subscription, table string) error {
	cols := s.columnsFor(ctx, table)

	rows, err := tx.QueryContext(ctx, "SELECT * FROM "+quoteIdent(table)) //nolint:gosec // quoted identifier sourced from sqlite_master; SQLite cannot bind table names
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

		change := config.Change{
			Source:   s.name,
			Op:       "snapshot",
			Schema:   "main",
			Table:    table,
			Relation: table,
			After:    mapRow(cols, vals),
		}
		if !sub.sendSnapshot(ctx, change) {
			return nil
		}
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
