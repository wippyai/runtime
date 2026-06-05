// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"fmt"

	"github.com/jackc/pglogrepl"
)

type decoder struct {
	rels      *relationCache
	commitLSN pglogrepl.LSN
	xid       uint32
}

func newDecoder() *decoder {
	return &decoder{rels: newRelationCache()}
}

func (d *decoder) decode(walData []byte, walStart pglogrepl.LSN) ([]RowChange, error) {
	msg, err := pglogrepl.Parse(walData)
	if err != nil {
		return nil, fmt.Errorf("parse logical message: %w", err)
	}
	return d.apply(msg, walStart)
}

func (d *decoder) apply(msg pglogrepl.Message, walStart pglogrepl.LSN) ([]RowChange, error) {
	switch m := msg.(type) {
	case *pglogrepl.RelationMessage:
		d.rels.put(m)
	case *pglogrepl.BeginMessage:
		d.commitLSN = m.FinalLSN
		d.xid = m.Xid
	case *pglogrepl.CommitMessage:
		d.commitLSN = 0
		d.xid = 0
	case *pglogrepl.InsertMessage:
		return d.one(OpInsert, m.RelationID, nil, m.Tuple, walStart)
	case *pglogrepl.UpdateMessage:
		return d.one(OpUpdate, m.RelationID, m.OldTuple, m.NewTuple, walStart)
	case *pglogrepl.DeleteMessage:
		return d.one(OpDelete, m.RelationID, m.OldTuple, nil, walStart)
	}
	return nil, nil
}

func (d *decoder) one(op Op, relID uint32, oldT, newT *pglogrepl.TupleData, walStart pglogrepl.LSN) ([]RowChange, error) {
	rel, ok := d.rels.get(relID)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownRelation, relID)
	}
	rc := RowChange{
		Op:        op,
		Schema:    rel.Namespace,
		Table:     rel.RelationName,
		LSN:       walStart.String(),
		CommitLSN: d.commitLSN.String(),
		XID:       d.xid,
		Before:    tupleToMap(rel, oldT),
		After:     tupleToMap(rel, newT),
	}
	return []RowChange{rc}, nil
}
