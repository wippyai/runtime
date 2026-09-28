// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
)

func (s *sqliteConnectionState) validateTable(schema, table string) error {
	if strings.EqualFold(schema, "temp") {
		return errors.New("sqlite mutation observer does not support TEMP tables")
	}
	query := sqliteMasterQuery(schema)
	rows, err := s.sqlite.Query(query, []driver.Value{table})
	if err != nil {
		return fmt.Errorf("inspect sqlite table %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	values := make([]driver.Value, len(rows.Columns()))
	if err := rows.Next(values); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("sqlite table %s.%s disappeared", schema, table)
		}
		return err
	}
	definition := ""
	switch value := values[0].(type) {
	case string:
		definition = value
	case []byte:
		definition = string(value)
	}
	upper := strings.ToUpper(definition)
	if strings.Contains(upper, "WITHOUT ROWID") {
		return fmt.Errorf("sqlite mutation observer does not support WITHOUT ROWID table %s.%s", schema, table)
	}
	if strings.HasPrefix(strings.TrimSpace(upper), "CREATE VIRTUAL TABLE") {
		return fmt.Errorf("sqlite mutation observer does not support virtual table %s.%s", schema, table)
	}
	return nil
}

func (s *sqliteConnectionState) tableColumns(schema, table string) ([]string, error) {
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT 0", quoteIdentifier(schema), quoteIdentifier(table))
	rows, err := s.sqlite.Query(query, nil)
	if err != nil {
		return nil, fmt.Errorf("read sqlite columns %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	return append([]string(nil), rows.Columns()...), nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// Unary + is a SQLite no-op that removes expression affinity and declaration
// metadata without changing the stored value. This prevents database/sql's
// DATE/BOOLEAN conveniences from changing snapshot values relative to hooks.
func storageProjection(columns []string) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		quoted := quoteIdentifier(column)
		parts[i] = "+" + quoted + " AS " + quoted
	}
	return strings.Join(parts, ", ")
}

func sqliteMasterQuery(schema string) string {
	return "SELECT sql FROM " + quoteIdentifier(schema) + ".sqlite_master WHERE type = 'table' AND name = ?"
}
