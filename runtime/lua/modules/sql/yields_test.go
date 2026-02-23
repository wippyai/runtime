// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"testing"
)

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
