// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"fmt"

	"github.com/jackc/pglogrepl"
)

type bufferedChange struct {
	rc     RowChange
	subxid uint32
}

type decoder struct {
	rels      *relationCache
	buffer    map[uint32][]bufferedChange
	commitLSN pglogrepl.LSN
	xid       uint32
	curTopXid uint32
	streaming bool
	inStream  bool
}

func newDecoder() *decoder {
	return &decoder{rels: newRelationCache()}
}

func newStreamingDecoder() *decoder {
	return &decoder{rels: newRelationCache(), streaming: true, buffer: map[uint32][]bufferedChange{}}
}

func (d *decoder) decode(walData []byte, walStart pglogrepl.LSN) ([]RowChange, error) {
	var (
		msg pglogrepl.Message
		err error
	)
	if d.streaming {
		msg, err = pglogrepl.ParseV2(walData, d.inStream)
	} else {
		msg, err = pglogrepl.Parse(walData)
	}
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
	case *pglogrepl.TruncateMessage:
		return d.truncate(m, walStart)
	case *pglogrepl.RelationMessageV2:
		d.rels.put(&m.RelationMessage)
	case *pglogrepl.StreamStartMessageV2:
		d.inStream = true
		d.curTopXid = m.Xid
		if _, ok := d.buffer[m.Xid]; !ok {
			d.buffer[m.Xid] = nil
		}
	case *pglogrepl.StreamStopMessageV2:
		d.inStream = false
	case *pglogrepl.StreamCommitMessageV2:
		return d.flushStream(m.Xid, m.CommitLSN), nil
	case *pglogrepl.StreamAbortMessageV2:
		d.abortStream(m.Xid, m.SubXid)
	case *pglogrepl.InsertMessageV2:
		if d.inStream {
			return nil, d.bufferOne(m.Xid, OpInsert, m.RelationID, nil, m.Tuple, walStart)
		}
		return d.one(OpInsert, m.RelationID, nil, m.Tuple, walStart)
	case *pglogrepl.UpdateMessageV2:
		if d.inStream {
			return nil, d.bufferOne(m.Xid, OpUpdate, m.RelationID, m.OldTuple, m.NewTuple, walStart)
		}
		return d.one(OpUpdate, m.RelationID, m.OldTuple, m.NewTuple, walStart)
	case *pglogrepl.DeleteMessageV2:
		if d.inStream {
			return nil, d.bufferOne(m.Xid, OpDelete, m.RelationID, m.OldTuple, nil, walStart)
		}
		return d.one(OpDelete, m.RelationID, m.OldTuple, nil, walStart)
	case *pglogrepl.TruncateMessageV2:
		if d.inStream {
			return nil, d.bufferTruncate(m, walStart)
		}
		return d.truncate(&m.TruncateMessage, walStart)
	}
	return nil, nil
}

func (d *decoder) changeFor(op Op, relID uint32, oldT, newT *pglogrepl.TupleData, walStart pglogrepl.LSN) (RowChange, error) {
	rel, ok := d.rels.get(relID)
	if !ok {
		return RowChange{}, fmt.Errorf("%w: %d", ErrUnknownRelation, relID)
	}
	return RowChange{
		Op:     op,
		Schema: rel.Namespace,
		Table:  rel.RelationName,
		LSN:    walStart.String(),
		Before: tupleToMap(rel, oldT),
		After:  tupleToMap(rel, newT),
	}, nil
}

func (d *decoder) one(op Op, relID uint32, oldT, newT *pglogrepl.TupleData, walStart pglogrepl.LSN) ([]RowChange, error) {
	rc, err := d.changeFor(op, relID, oldT, newT, walStart)
	if err != nil {
		return nil, err
	}
	rc.XID = d.xid
	rc.CommitLSN = d.commitLSN.String()
	return []RowChange{rc}, nil
}

func (d *decoder) bufferOne(subxid uint32, op Op, relID uint32, oldT, newT *pglogrepl.TupleData, walStart pglogrepl.LSN) error {
	rc, err := d.changeFor(op, relID, oldT, newT, walStart)
	if err != nil {
		return err
	}
	rc.XID = d.curTopXid
	d.buffer[d.curTopXid] = append(d.buffer[d.curTopXid], bufferedChange{rc: rc, subxid: subxid})
	return nil
}

func (d *decoder) bufferTruncate(m *pglogrepl.TruncateMessageV2, walStart pglogrepl.LSN) error {
	for _, relID := range m.RelationIDs {
		rel, ok := d.rels.get(relID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrUnknownRelation, relID)
		}
		rc := RowChange{
			Op:     OpTruncate,
			Schema: rel.Namespace,
			Table:  rel.RelationName,
			LSN:    walStart.String(),
			XID:    d.curTopXid,
		}
		d.buffer[d.curTopXid] = append(d.buffer[d.curTopXid], bufferedChange{rc: rc, subxid: m.Xid})
	}
	return nil
}

func (d *decoder) flushStream(topXid uint32, commitLSN pglogrepl.LSN) []RowChange {
	buffered := d.buffer[topXid]
	delete(d.buffer, topXid)
	d.inStream = false

	out := make([]RowChange, 0, len(buffered))
	for i := range buffered {
		buffered[i].rc.CommitLSN = commitLSN.String()
		out = append(out, buffered[i].rc)
	}
	return out
}

func (d *decoder) abortStream(topXid, subXid uint32) {
	d.inStream = false
	if topXid == subXid {
		delete(d.buffer, topXid)
		return
	}
	src := d.buffer[topXid]
	kept := src[:0]
	for _, bc := range src {
		if bc.subxid != subXid {
			kept = append(kept, bc)
		}
	}
	d.buffer[topXid] = kept
}

func (d *decoder) truncate(m *pglogrepl.TruncateMessage, walStart pglogrepl.LSN) ([]RowChange, error) {
	changes := make([]RowChange, 0, len(m.RelationIDs))
	for _, relID := range m.RelationIDs {
		rel, ok := d.rels.get(relID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownRelation, relID)
		}
		changes = append(changes, RowChange{
			Op:        OpTruncate,
			Schema:    rel.Namespace,
			Table:     rel.RelationName,
			LSN:       walStart.String(),
			CommitLSN: d.commitLSN.String(),
			XID:       d.xid,
		})
	}
	return changes, nil
}
