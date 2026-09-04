// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"errors"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func (s *sqliteConnectionState) applyStatementMeta(meta statementMeta) {
	if s.prepareMeta.savepointCount > 1 || meta.savepointCount > 1 {
		s.failAmbiguous()
	}
	if s.prepareMeta.ddl || meta.ddl {
		s.statementDDL = true
	}
	if s.prepareMeta.unsupported != nil {
		s.unsupported = s.prepareMeta.unsupported
	}
	if meta.unsupported != nil {
		s.unsupported = meta.unsupported
	}
	if s.prepareMeta.savepointVerb != "" {
		s.statementSavepointVerb = s.prepareMeta.savepointVerb
		s.statementSavepointName = s.prepareMeta.savepointName
	}
	if meta.savepointVerb != "" {
		s.statementSavepointVerb = meta.savepointVerb
		s.statementSavepointName = meta.savepointName
	}
	s.prepareMeta = statementMeta{}
}

func (s *sqliteConnectionState) failAmbiguous() {
	s.failed = errObserverAmbiguous
	if s.backend.hasObservers() {
		s.backend.fail(errObserverAmbiguous)
	}
}

func (s *sqliteConnectionState) finalizeAfterError(_ string, err error) {
	s.statementEndWithMeta(err, statementMeta{})
}

// finalize runs only after SQLite has returned from the operation that caused
// the commit hook. The hook is therefore a candidate marker; it never emits
// data itself. Net reduction uses the hook images collected on this physical
// connection, after statement/savepoint outcomes are known.
func (s *sqliteConnectionState) finalize() {
	if !s.commitPending {
		return
	}
	s.commitPending = false
	defer func() {
		if s.fenceHeld {
			s.fenceHeld = false
			s.backend.releaseFence()
		}
	}()
	if !s.backend.hasObservers() {
		s.resetTransaction()
		return
	}
	if s.failed != nil {
		s.backend.fail(s.failed)
		s.resetTransaction()
		return
	}
	if s.ddlInTxn && s.dmlInTxn {
		s.backend.fail(errors.New("sqlite mutation observer cannot represent DDL and DML in one transaction"))
		s.resetTransaction()
		return
	}
	ends := s.commitEnds
	if len(ends) == 0 {
		ends = []int{len(s.pending)}
	}
	start := 0
	for _, end := range ends {
		if end < start || end > len(s.pending) {
			s.backend.fail(errors.New("sqlite mutation observer commit boundary is invalid"))
			s.resetTransaction()
			return
		}
		changes, err := s.netChangesFor(s.pending[start:end])
		if err != nil {
			s.backend.fail(err)
			s.resetTransaction()
			return
		}
		mutations := make([]sqlapi.Mutation, len(changes))
		for i := range changes {
			mutations[i] = changes[i].Mutation
		}
		s.backend.publish(mutations)
		start = end
	}
	s.resetTransaction()
}

func (s *sqliteConnectionState) resetTransaction() {
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
	s.ddlInTxn = false
	s.dmlInTxn = false
	s.statementDDL = false
	s.unsupported = nil
	s.rollbackSeen = false
	s.rollbackUnconfirmed = false
	s.prepareMeta = statementMeta{}
}

// Savepoint state is applied only after SQLite reports success. A failed
// ROLLBACK TO/RELEASE must not change the candidate transaction in memory.
func (s *sqliteConnectionState) applySavepoint() {
	verb, name := s.statementSavepointVerb, s.statementSavepointName
	switch verb {
	case "savepoint":
		s.savepoints = append(s.savepoints, savepoint{name: name, index: len(s.pending)})
	case "rollback to":
		if index, ok := s.findSavepoint(name); ok {
			s.pending = s.pending[:s.savepoints[index].index]
			s.recomputePendingBytes()
			s.savepoints = s.savepoints[:index+1]
		}
	case "release":
		if index, ok := s.findSavepoint(name); ok {
			s.savepoints = s.savepoints[:index]
		}
	}
}

func (s *sqliteConnectionState) recomputePendingBytes() {
	s.pendingBytes = 0
	for _, change := range s.pending {
		s.pendingBytes += mutationSize(change.Mutation)
	}
}

type mutationKey struct {
	schema string
	table  string
	rowID  int64
}

// netChanges retains the earliest before-image for each row and the latest
// pre-update after-image. SQLite invokes the hook for every trigger-generated
// row change too, so the latest image is the committed row state without an
// O(N) SELECT round trip during the commit fence. Table metadata is resolved
// once per touched table.
func (s *sqliteConnectionState) netChangesFor(pending []capturedMutation) ([]capturedMutation, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	if s.sqlite == nil {
		return nil, errors.New("sqlite mutation observer has no physical connection")
	}
	columnsByTable := make(map[string][]string)
	for i := range pending {
		change := &pending[i]
		tableKey := change.Schema + "\x00" + change.Table
		columns, ok := columnsByTable[tableKey]
		if !ok {
			if err := s.validateTable(change.Schema, change.Table); err != nil {
				return nil, err
			}
			var err error
			columns, err = s.tableColumns(change.Schema, change.Table)
			if err != nil {
				return nil, err
			}
			columnsByTable[tableKey] = columns
		}
		change.Columns = columns
	}
	return coalesceMutations(pending), nil
}

func (s *sqliteConnectionState) findSavepoint(name string) (int, bool) {
	for i := len(s.savepoints) - 1; i >= 0; i-- {
		if s.savepoints[i].name == name {
			return i, true
		}
	}
	return 0, false
}
