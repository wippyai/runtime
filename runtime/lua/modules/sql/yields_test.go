// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestQueryResultTimestampPrecision(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	for _, value := range []string{
		"2026-10-13T10:12:00Z",
		"2026-10-13T10:12:00.000001Z",
		"2026-10-13T10:12:00.123456Z",
		"2026-10-13T10:12:00.123456789Z",
		"2026-10-13T10:12:00.123456789-04:00",
	} {
		t.Run(value, func(t *testing.T) {
			stamp, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				t.Fatal(err)
			}
			rows := queryResultToLua(l, sqlapi.QueryResponse{
				Columns: []string{"date"}, Rows: [][]any{{stamp}},
			}).(*lua.LTable)
			row := rows.RawGetInt(1).(*lua.LTable)
			actual := row.RawGetString("date").String()
			expected := stamp.UTC().Format(time.RFC3339Nano)
			if actual != expected {
				t.Fatalf("timestamp lost precision: got %q, want %q", actual, expected)
			}
			roundTrip, parseErr := time.Parse(time.RFC3339Nano, actual)
			if parseErr != nil || !roundTrip.Equal(stamp) {
				t.Fatalf("timestamp did not round-trip: %q (%v)", actual, parseErr)
			}
		})
	}
}

func TestAcquireQueryYield(t *testing.T) {
	yield := AcquireQueryYield()
	if yield == nil {
		t.Error("expected non-nil QueryYield")
	}
}

func TestAcquireExecuteYield(t *testing.T) {
	yield := AcquireExecuteYield()
	if yield == nil {
		t.Error("expected non-nil ExecuteYield")
	}
}

func TestAcquirePrepareYield(t *testing.T) {
	yield := AcquirePrepareYield()
	if yield == nil {
		t.Error("expected non-nil PrepareYield")
	}
}

func TestAcquireBeginYield(t *testing.T) {
	yield := AcquireBeginYield()
	if yield == nil {
		t.Error("expected non-nil BeginYield")
	}
}

func TestAcquireStmtQueryYield(t *testing.T) {
	yield := AcquireStmtQueryYield()
	if yield == nil {
		t.Error("expected non-nil StmtQueryYield")
	}
}

func TestAcquireStmtExecuteYield(t *testing.T) {
	yield := AcquireStmtExecuteYield()
	if yield == nil {
		t.Error("expected non-nil StmtExecuteYield")
	}
}

func TestAcquireStmtCloseYield(t *testing.T) {
	yield := AcquireStmtCloseYield()
	if yield == nil {
		t.Error("expected non-nil StmtCloseYield")
	}
}

func TestAcquireTxQueryYield(t *testing.T) {
	yield := AcquireTxQueryYield()
	if yield == nil {
		t.Error("expected non-nil TxQueryYield")
	}
}

func TestAcquireTxExecuteYield(t *testing.T) {
	yield := AcquireTxExecuteYield()
	if yield == nil {
		t.Error("expected non-nil TxExecuteYield")
	}
}

func TestAcquireTxSavepointYield(t *testing.T) {
	yield := AcquireTxSavepointYield()
	if yield == nil {
		t.Error("expected non-nil TxSavepointYield")
	}
}

func TestAcquireTxPrepareYield(t *testing.T) {
	yield := AcquireTxPrepareYield()
	if yield == nil {
		t.Error("expected non-nil TxPrepareYield")
	}
}

func TestAcquireTxCommitYield(t *testing.T) {
	yield := AcquireTxCommitYield()
	if yield == nil {
		t.Error("expected non-nil TxCommitYield")
	}
}

func TestAcquireTxRollbackYield(t *testing.T) {
	yield := AcquireTxRollbackYield()
	if yield == nil {
		t.Error("expected non-nil TxRollbackYield")
	}
}
