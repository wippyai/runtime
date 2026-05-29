// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"database/sql"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestCheckDBValid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	db := &DB{
		db:     &sql.DB{},
		dbType: "db.sql.postgres",
	}

	ud := l.NewUserData()
	ud.Value = db
	l.Push(ud)

	result := checkDB(l)
	if result == nil {
		t.Error("expected non-nil DB")
	}
	if result != db {
		t.Error("expected same DB instance")
	}
}

func TestDBGetRawDB(t *testing.T) {
	mockDB := &sql.DB{}
	db := &DB{
		db:     mockDB,
		dbType: "db.sql.postgres",
	}

	result := db.GetRawDB()
	if result != mockDB {
		t.Error("expected same *sql.DB instance")
	}
}

func TestDBGetDBType(t *testing.T) {
	db := &DB{
		db:     &sql.DB{},
		dbType: "db.sql.mysql",
	}

	result := db.GetDBType()
	if result != "db.sql.mysql" {
		t.Errorf("expected 'db.sql.mysql', got %s", result)
	}
}

func TestMapDBTypeFromResourceKind(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"db.sql.postgres", TypePostgres},
		{"db.sql.mysql", TypeMySQL},
		{"db.sql.sqlite", TypeSQLite},
		{"unknown", TypeUnknown},
		{"", TypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapDBTypeFromResourceKind(tt.input)
			if result != tt.expected {
				t.Errorf("mapDBTypeFromResourceKind(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}
