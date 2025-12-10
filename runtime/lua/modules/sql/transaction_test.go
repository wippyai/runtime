package sql

import (
	"database/sql"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestCheckTransactionValid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	db := &DB{
		db:     &sql.DB{},
		dbType: "db.sql.postgres",
	}

	tx := &Transaction{
		tx:     &sql.Tx{},
		db:     db,
		active: true,
	}

	ud := l.NewUserData()
	ud.Value = tx
	l.Push(ud)

	result := checkTransaction(l)
	if result == nil {
		t.Error("expected non-nil Transaction")
	}
	if result != tx {
		t.Error("expected same Transaction instance")
	}
}

func TestTransactionGetRawTx(t *testing.T) {
	mockTx := &sql.Tx{}
	tx := &Transaction{
		tx:     mockTx,
		active: true,
	}

	result := tx.GetRawTx()
	if result != mockTx {
		t.Error("expected same *sql.Tx instance")
	}
}

func TestTransactionGetDBType(t *testing.T) {
	db := &DB{
		db:     &sql.DB{},
		dbType: "db.sql.mysql",
	}

	tx := &Transaction{
		tx:     &sql.Tx{},
		db:     db,
		active: true,
	}

	result := tx.GetDBType()
	if result != "db.sql.mysql" {
		t.Errorf("expected 'db.sql.mysql', got %s", result)
	}
}

func TestIsValidSavepointName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid alphanumeric", "savepoint123", true},
		{"valid with underscore", "save_point_1", true},
		{"valid uppercase", "SAVEPOINT", true},
		{"valid mixed case", "SavePoint_123", true},
		{"invalid with space", "save point", false},
		{"invalid with dash", "save-point", false},
		{"invalid with special char", "save@point", false},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidSavepointName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidSavepointName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
