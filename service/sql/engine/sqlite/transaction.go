// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// sqliteConnectionState is attached to one physical SQLite connection. The
// hooks only collect a candidate transaction. Publication happens from the
// driver wrappers after Exec/Commit/Rows completion, when statement rollback
// and savepoint effects are known.
type sqliteConnectionState struct {
	unsupported            error
	failed                 error
	sqlite                 *sqlite3.SQLiteConn
	backend                *sqliteBackend
	statementSavepointVerb string
	statementSavepointName string
	savepoints             []savepoint
	pending                []capturedMutation
	commitEnds             []int
	prepareMeta            statementMeta
	statementMark          int
	maxBytes               int
	maxChanges             int
	pendingBytes           int
	confirmedEnds          int
	maxCommitEnds          int
	rollbackSeen           bool
	ddlInTxn               bool
	dmlInTxn               bool
	fenceHeld              bool
	statementDDL           bool
	commitPending          bool
	rollbackUnconfirmed    bool
}

// statementMeta is collected by SQLite's authorizer while a statement is
// prepared. It avoids interpreting comments, literals, or quoted identifiers
// as executable SQL control words. A prepared statement carries this metadata
// to its later execution; direct Exec/Query paths merge it after the driver's
// native prepare loop returns.
type statementMeta struct {
	unsupported    error
	savepointVerb  string
	savepointName  string
	savepointCount int
	ddl            bool
}

type savepoint struct {
	name  string
	index int
}

func (s *sqliteConnectionState) bind(conn *sqlite3.SQLiteConn) {
	conn.RegisterPreUpdateHook(s.preUpdate)
	conn.RegisterCommitHook(s.commit)
	conn.RegisterRollbackHook(s.rollback)
	conn.RegisterAuthorizer(s.authorizer)
	s.sqlite = conn
}

func (s *sqliteConnectionState) authorizer(action int, arg1, arg2, _ string) int {
	// Reaching authorizer for another prepared statement proves that any
	// earlier commit-hook boundary belongs to a completed statement. This is
	// the native boundary signal needed when a later statement in one Exec
	// fails before its own pre-update hook runs.
	if len(s.commitEnds) > s.confirmedEnds {
		s.confirmedEnds = len(s.commitEnds)
	}
	switch action {
	case sqlite3.SQLITE_CREATE_INDEX,
		sqlite3.SQLITE_CREATE_TABLE,
		sqlite3.SQLITE_CREATE_TEMP_INDEX,
		sqlite3.SQLITE_CREATE_TEMP_TABLE,
		sqlite3.SQLITE_CREATE_TEMP_TRIGGER,
		sqlite3.SQLITE_CREATE_TEMP_VIEW,
		sqlite3.SQLITE_CREATE_TRIGGER,
		sqlite3.SQLITE_CREATE_VIEW,
		sqlite3.SQLITE_DROP_INDEX,
		sqlite3.SQLITE_DROP_TABLE,
		sqlite3.SQLITE_DROP_TEMP_INDEX,
		sqlite3.SQLITE_DROP_TEMP_TABLE,
		sqlite3.SQLITE_DROP_TEMP_TRIGGER,
		sqlite3.SQLITE_DROP_TEMP_VIEW,
		sqlite3.SQLITE_DROP_TRIGGER,
		sqlite3.SQLITE_DROP_VIEW,
		sqlite3.SQLITE_ALTER_TABLE,
		sqlite3.SQLITE_ATTACH,
		sqlite3.SQLITE_DETACH:
		s.prepareMeta.ddl = true
	case sqlite3.SQLITE_CREATE_VTABLE:
		s.prepareMeta.ddl = true
		s.prepareMeta.unsupported = fmt.Errorf("sqlite mutation observer cannot observe virtual table %s", arg1)
	case sqlite3.SQLITE_DROP_VTABLE:
		s.prepareMeta.ddl = true
		s.prepareMeta.unsupported = fmt.Errorf("sqlite mutation observer cannot observe dropped virtual table %s", arg1)
	case sqlite3.SQLITE_SAVEPOINT:
		verb, name := authorizerSavepoint(arg1, arg2)
		if verb != "" {
			s.prepareMeta.savepointCount++
			s.prepareMeta.savepointVerb = verb
			s.prepareMeta.savepointName = name
		}
	}
	return sqlite3.SQLITE_OK
}

func authorizerSavepoint(operation, name string) (string, string) {
	switch strings.ToLower(operation) {
	case "begin":
		return "savepoint", normalizeSavepointName(name)
	case "rollback":
		return "rollback to", normalizeSavepointName(name)
	case "release":
		return "release", normalizeSavepointName(name)
	default:
		return "", ""
	}
}

func (s *sqliteConnectionState) preUpdate(data sqlite3.SQLitePreUpdateData) {
	if s.failed != nil || s.statementDDL || strings.HasPrefix(strings.ToLower(data.TableName), "sqlite_") {
		return
	}
	// A later statement can only reach pre-update after the preceding
	// autocommit callback returned successfully. Confirm that preceding fence
	// boundary before collecting the new candidate.
	if len(s.commitEnds) > s.confirmedEnds {
		s.confirmedEnds = len(s.commitEnds)
	}
	count := data.Count()
	var before, after []any
	var err error
	switch data.Op {
	case sqlite3.SQLITE_INSERT:
		after, err = scanSQLiteRow(&data, count, true)
	case sqlite3.SQLITE_UPDATE:
		before, err = scanSQLiteRow(&data, count, false)
		if err == nil {
			after, err = scanSQLiteRow(&data, count, true)
		}
	case sqlite3.SQLITE_DELETE:
		before, err = scanSQLiteRow(&data, count, false)
	}
	if err != nil {
		s.failed = err
		return
	}

	op := "unknown"
	switch data.Op {
	case sqlite3.SQLITE_INSERT:
		op = "insert"
	case sqlite3.SQLITE_UPDATE:
		op = "update"
	case sqlite3.SQLITE_DELETE:
		op = "delete"
	}
	s.pending = append(s.pending, capturedMutation{
		Mutation: sqlapi.Mutation{
			Schema: data.DatabaseName,
			Table:  data.TableName,
			Before: before,
			After:  after,
			Op:     op,
		},
		OldRowID: data.OldRowID,
		RowID:    data.NewRowID,
	})
	s.pendingBytes = saturatingAdd(s.pendingBytes, mutationSize(s.pending[len(s.pending)-1].Mutation))
	if (s.maxChanges > 0 && len(s.pending) > s.maxChanges) || (s.maxBytes > 0 && s.pendingBytes > s.maxBytes) {
		s.failed = errObserverOverflow
		return
	}
	s.dmlInTxn = true
}

func (s *sqliteConnectionState) commit() int {
	// A single go-sqlite3 ExecContext may execute several semicolon-separated
	// statements inside one C call. Reuse the fence when SQLite invokes the
	// commit hook more than once before the wrapper gets control back; the
	// wrapper publishes the combined candidate and releases it exactly once.
	if !s.fenceHeld {
		s.backend.acquireCommitFence()
		s.fenceHeld = true
	}
	s.commitPending = true
	if s.maxCommitEnds > 0 && len(s.commitEnds) >= s.maxCommitEnds {
		s.failed = errObserverOverflow
		return 0
	}
	s.commitEnds = append(s.commitEnds, len(s.pending))
	return 0
}

func (s *sqliteConnectionState) rollback() {
	s.rollbackSeen = true
	// A failed ROLLBACK TO/RELEASE SAVEPOINT can invoke SQLite's rollback
	// hook even though the surrounding transaction remains active. The
	// authorizer metadata identifies that control statement; defer bookkeeping
	// until the wrapper sees its actual error instead of discarding the outer
	// transaction candidate here.
	if s.prepareMeta.savepointVerb != "" || s.statementSavepointVerb != "" {
		return
	}
	if len(s.commitEnds) > s.confirmedEnds {
		s.rollbackUnconfirmed = true
	}
	if len(s.commitEnds) > 0 {
		s.commitPending = true
		s.failed = nil
		return
	}
	s.pending = nil
	s.pendingBytes = 0
	s.savepoints = nil
	s.statementMark = 0
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	s.prepareMeta = statementMeta{}
	s.commitPending = false
	s.commitEnds = nil
	s.confirmedEnds = 0
	if s.fenceHeld {
		s.fenceHeld = false
		s.backend.releaseFence()
	}
	s.ddlInTxn = false
	s.dmlInTxn = false
	s.failed = nil
	s.statementDDL = false
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	s.prepareMeta = statementMeta{}
	s.unsupported = nil
}

func (s *sqliteConnectionState) statementBegin(_ string) {
	s.statementBeginWithMeta(statementMeta{})
}

func (s *sqliteConnectionState) statementBeginWithMeta(meta statementMeta) {
	s.statementMark = len(s.pending)
	s.statementDDL = meta.ddl
	s.statementSavepointVerb = meta.savepointVerb
	s.statementSavepointName = meta.savepointName
	s.rollbackSeen = false
	s.rollbackUnconfirmed = false
	s.prepareMeta = statementMeta{}
}

func (s *sqliteConnectionState) statementEnd(_ string, err error) {
	s.statementEndWithMeta(err, statementMeta{})
}

func (s *sqliteConnectionState) statementEndWithMeta(err error, meta statementMeta) {
	s.applyStatementMeta(meta)
	if s.rollbackUnconfirmed {
		s.resolveRollbackOutcome()
	}
	if err == nil {
		s.confirmedEnds = len(s.commitEnds)
	} else if !s.rollbackSeen {
		// A commit hook boundary is authoritative once the driver call has
		// returned without a rollback callback. Keep confirmed prefixes even
		// when a later native statement in the same call reports an error.
		s.confirmedEnds = len(s.commitEnds)
	}
	lastBoundary := 0
	if len(s.commitEnds) > 0 {
		lastBoundary = s.commitEnds[len(s.commitEnds)-1]
	}
	residual := len(s.pending) > s.statementMark
	if len(s.commitEnds) > 0 {
		residual = len(s.pending) > lastBoundary
	}
	if err != nil && !s.rollbackSeen && residual {
		// SQLite's pre-update hook does not expose whether a failed DML
		// statement used ABORT or FAIL. Do not infer the conflict mode from
		// caller SQL; closing the observer is safer than publishing a partial
		// candidate whose commit status is unknown.
		s.failAmbiguous()
		truncate := s.statementMark
		if lastBoundary > truncate {
			truncate = lastBoundary
		}
		if truncate <= len(s.pending) {
			s.pending = s.pending[:truncate]
		}
		s.recomputePendingBytes()
	}
	s.statementMark = 0
	s.rollbackSeen = false
	s.rollbackUnconfirmed = false
	if err == nil {
		if s.statementDDL {
			s.ddlInTxn = true
		}
		s.applySavepoint()
	}
	s.statementDDL = false
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	if s.unsupported != nil && s.backend.hasObservers() {
		s.backend.fail(s.unsupported)
	}
	s.unsupported = nil
	if s.failed != nil {
		if s.commitPending {
			s.finalize()
		}
		return
	}
	if s.commitPending {
		s.finalize()
	}
}

// resolveRollbackOutcome runs after SQLite has returned from the physical
// operation. A rollback hook can follow a committed autocommit prefix when a
// later native statement in the same Exec fails, but it can also follow a
// failed physical commit. The hook alone cannot distinguish those cases. The
// wrapper therefore verifies the unconfirmed net rows on the same connection;
// if they are not provably committed, the observer fails closed.
func (s *sqliteConnectionState) resolveRollbackOutcome() {
	if !s.rollbackUnconfirmed {
		return
	}
	if s.sqlite == nil || !s.sqlite.AutoCommit() {
		s.failAmbiguous()
		return
	}
	base := 0
	if s.confirmedEnds > 0 {
		base = s.commitEnds[s.confirmedEnds-1]
	}
	if base > len(s.pending) {
		s.failAmbiguous()
		return
	}
	changes, err := s.netChangesFor(s.pending[base:])
	if err != nil {
		s.failAmbiguous()
		return
	}
	committed, err := s.finalRowsMatch(changes)
	if err != nil {
		s.failAmbiguous()
		return
	}
	if !committed {
		if s.confirmedEnds > 0 {
			s.pending = s.pending[:base]
			s.recomputePendingBytes()
			s.commitEnds = s.commitEnds[:s.confirmedEnds]
			s.rollbackUnconfirmed = false
			return
		}
		s.failAmbiguous()
		return
	}
	s.confirmedEnds = len(s.commitEnds)
	s.rollbackUnconfirmed = false
}
