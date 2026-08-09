// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"fmt"

	"github.com/jackc/pglogrepl"
)

type bufferedChange struct {
	rc     RowChange
	subxid uint32
	bytes  int64
}

// decodeResult describes the progress made by one logical replication
// message. Changes are returned only at a transaction boundary. A safe result
// means the WAL position after this message can be acknowledged without
// reconstructing decoder state after a restart.
type decodeResult struct {
	changes []RowChange
	safe    bool
}

type decoder struct {
	rels            *relationCache
	buffer          map[uint32][]bufferedChange
	usage           map[uint32]int64
	limits          decoderLimits
	commitLSN       pglogrepl.LSN
	inflightChanges int
	inflightBytes   int64
	xid             uint32
	curTopXid       uint32
	streaming       bool
	inStream        bool
	txActive        bool
}

func newDecoder(limits ...decoderLimits) *decoder {
	return newDecoderWithMode(false, limits...)
}

func newStreamingDecoder(limits ...decoderLimits) *decoder {
	return newDecoderWithMode(true, limits...)
}

func newDecoderWithMode(streaming bool, limits ...decoderLimits) *decoder {
	configured := defaultDecoderLimits()
	if len(limits) > 0 {
		configured = normalizeDecoderLimits(limits[0])
	}
	return &decoder{
		rels:      newRelationCache(),
		buffer:    make(map[uint32][]bufferedChange),
		streaming: streaming,
		limits:    configured,
		usage:     make(map[uint32]int64),
	}
}

func (d *decoder) decodeResult(walData []byte, walStart pglogrepl.LSN) (decodeResult, error) {
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
		return decodeResult{}, fmt.Errorf("parse logical message: %w", err)
	}
	return d.applyResult(msg, walStart)
}

// apply is retained as the small decoder test seam. It deliberately returns
// no rows until Commit or StreamCommit; callers that need checkpoint progress
// use applyResult/decodeResult instead.
func (d *decoder) apply(msg pglogrepl.Message, walStart pglogrepl.LSN) ([]RowChange, error) {
	result, err := d.applyResult(msg, walStart)
	if err != nil {
		return nil, err
	}
	return result.changes, nil
}

func (d *decoder) applyResult(msg pglogrepl.Message, walStart pglogrepl.LSN) (decodeResult, error) {
	switch m := msg.(type) {
	case *pglogrepl.RelationMessage:
		d.rels.put(m)
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.OriginMessage:
		// Origin is transaction metadata. The row API has no origin field, but
		// the message is valid and must not interrupt an otherwise valid stream.
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.TypeMessage:
		// Type definitions are metadata for output plugins. Tuple decoding is
		// intentionally text-preserving, so there is no row-level state to
		// update here.
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.LogicalDecodingMessage:
		// Logical messages are valid pgoutput records but are not row changes.
		// They remain commit-gated by the decoder state and are therefore safe
		// to ignore without advancing a checkpoint inside a transaction.
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.BeginMessage:
		// Protocol v2 may interleave an ordinary (small) transaction with
		// streamed transactions whose segments have already been stopped. The
		// ordinary transaction has its own buffer (key 0), so an existing
		// streamed buffer is not a nested Begin.
		if d.txActive || d.inStream {
			return decodeResult{}, fmt.Errorf("%w: nested begin", ErrInvalidTransaction)
		}
		d.commitLSN = m.FinalLSN
		d.xid = m.Xid
		d.txActive = true
		d.buffer[0] = nil
		return decodeResult{}, nil

	case *pglogrepl.CommitMessage:
		if !d.txActive {
			return decodeResult{}, fmt.Errorf("%w: commit without begin", ErrInvalidTransaction)
		}
		return decodeResult{changes: d.flushTransaction(m.CommitLSN), safe: len(d.buffer) == 0}, nil

	case *pglogrepl.InsertMessage:
		return d.one(OpInsert, m.RelationID, nil, m.Tuple, walStart)

	case *pglogrepl.UpdateMessage:
		return d.one(OpUpdate, m.RelationID, m.OldTuple, m.NewTuple, walStart)

	case *pglogrepl.DeleteMessage:
		return d.one(OpDelete, m.RelationID, m.OldTuple, nil, walStart)

	case *pglogrepl.TruncateMessage:
		return d.truncateResult(m.RelationIDs, walStart)

	case *pglogrepl.RelationMessageV2:
		d.rels.put(&m.RelationMessage)
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.TypeMessageV2:
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.LogicalDecodingMessageV2:
		return decodeResult{safe: !d.inTransaction()}, nil

	case *pglogrepl.StreamStartMessageV2:
		if !d.streaming {
			return decodeResult{}, fmt.Errorf("%w: stream start in protocol v1", ErrInvalidTransaction)
		}
		if d.inStream || d.txActive {
			return decodeResult{}, fmt.Errorf("%w: nested stream start", ErrInvalidTransaction)
		}
		d.inStream = true
		d.curTopXid = m.Xid
		if _, ok := d.buffer[m.Xid]; !ok {
			d.buffer[m.Xid] = nil
		}
		return decodeResult{}, nil

	case *pglogrepl.StreamStopMessageV2:
		if !d.inStream {
			return decodeResult{}, fmt.Errorf("%w: stream stop without start", ErrInvalidTransaction)
		}
		d.inStream = false
		return decodeResult{}, nil

	case *pglogrepl.StreamCommitMessageV2:
		if d.inStream {
			return decodeResult{}, fmt.Errorf("%w: stream commit before stream stop", ErrInvalidTransaction)
		}
		if _, ok := d.buffer[m.Xid]; !ok {
			return decodeResult{}, fmt.Errorf("%w: stream commit for unknown xid %d", ErrInvalidTransaction, m.Xid)
		}
		changes := d.flushStream(m.Xid, m.CommitLSN)
		return decodeResult{changes: changes, safe: len(d.buffer) == 0}, nil

	case *pglogrepl.StreamAbortMessageV2:
		if d.inStream {
			return decodeResult{}, fmt.Errorf("%w: stream abort before stream stop", ErrInvalidTransaction)
		}
		if _, ok := d.buffer[m.Xid]; !ok {
			return decodeResult{}, fmt.Errorf("%w: stream abort for unknown xid %d", ErrInvalidTransaction, m.Xid)
		}
		d.abortStream(m.Xid, m.SubXid)
		return decodeResult{safe: len(d.buffer) == 0}, nil

	case *pglogrepl.InsertMessageV2:
		if d.inStream {
			return decodeResult{}, d.bufferOne(m.Xid, OpInsert, m.RelationID, nil, m.Tuple, walStart)
		}
		return d.one(OpInsert, m.RelationID, nil, m.Tuple, walStart)

	case *pglogrepl.UpdateMessageV2:
		if d.inStream {
			return decodeResult{}, d.bufferOne(m.Xid, OpUpdate, m.RelationID, m.OldTuple, m.NewTuple, walStart)
		}
		return d.one(OpUpdate, m.RelationID, m.OldTuple, m.NewTuple, walStart)

	case *pglogrepl.DeleteMessageV2:
		if d.inStream {
			return decodeResult{}, d.bufferOne(m.Xid, OpDelete, m.RelationID, m.OldTuple, nil, walStart)
		}
		return d.one(OpDelete, m.RelationID, m.OldTuple, nil, walStart)

	case *pglogrepl.TruncateMessageV2:
		if d.inStream {
			return decodeResult{}, d.bufferTruncate(m, walStart)
		}
		return d.truncateResult(m.RelationIDs, walStart)

	default:
		return decodeResult{}, fmt.Errorf("%w: %T", ErrUnsupportedMessage, msg)
	}
}

func (d *decoder) inTransaction() bool {
	return d.txActive || d.inStream || len(d.buffer) > 0
}

func (d *decoder) flushTransaction(commitLSN pglogrepl.LSN) []RowChange {
	if commitLSN == 0 {
		commitLSN = d.commitLSN
	}
	buffered := d.buffer[0]
	d.releaseBuffer(0)
	changes := make([]RowChange, 0, len(buffered))
	for i := range buffered {
		buffered[i].rc.CommitLSN = commitLSN.String()
		changes = append(changes, buffered[i].rc)
	}
	d.commitLSN = 0
	d.xid = 0
	d.txActive = false
	return changes
}

func (d *decoder) one(op Op, relID uint32, oldT, newT *pglogrepl.TupleData, walStart pglogrepl.LSN) (decodeResult, error) {
	if !d.txActive {
		return decodeResult{}, fmt.Errorf("%w: row without begin", ErrInvalidTransaction)
	}
	bytes, err := d.reserveRow(0, relID, oldT, newT)
	if err != nil {
		return decodeResult{}, err
	}
	rc, err := d.changeFor(op, relID, oldT, newT, walStart)
	if err != nil {
		d.releaseReservation(0, 1, bytes)
		return decodeResult{}, err
	}
	rc.XID = d.xid
	rc.CommitLSN = d.commitLSN.String()
	d.buffer[0] = append(d.buffer[0], bufferedChange{rc: rc, subxid: d.xid, bytes: bytes})
	return decodeResult{}, nil
}

func (d *decoder) truncateResult(relationIDs []uint32, walStart pglogrepl.LSN) (decodeResult, error) {
	if !d.txActive {
		return decodeResult{}, fmt.Errorf("%w: truncate without begin", ErrInvalidTransaction)
	}
	relations, bytes, err := d.truncateBudget(0, relationIDs)
	if err != nil {
		return decodeResult{}, err
	}
	for i, rel := range relations {
		d.buffer[0] = append(d.buffer[0], bufferedChange{rc: RowChange{
			Op:        OpTruncate,
			Schema:    rel.Namespace,
			Table:     rel.RelationName,
			LSN:       walStart.String(),
			CommitLSN: d.commitLSN.String(),
			XID:       d.xid,
		}, subxid: d.xid, bytes: bytes[i]})
	}
	return decodeResult{}, nil
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

func (d *decoder) bufferOne(subxid uint32, op Op, relID uint32, oldT, newT *pglogrepl.TupleData, walStart pglogrepl.LSN) error {
	if !d.inStream {
		return fmt.Errorf("%w: streamed row without stream start", ErrInvalidTransaction)
	}
	bytes, err := d.reserveRow(d.curTopXid, relID, oldT, newT)
	if err != nil {
		return err
	}
	rc, err := d.changeFor(op, relID, oldT, newT, walStart)
	if err != nil {
		d.releaseReservation(d.curTopXid, 1, bytes)
		return err
	}
	rc.XID = d.curTopXid
	d.buffer[d.curTopXid] = append(d.buffer[d.curTopXid], bufferedChange{rc: rc, subxid: subxid, bytes: bytes})
	return nil
}

func (d *decoder) bufferTruncate(m *pglogrepl.TruncateMessageV2, walStart pglogrepl.LSN) error {
	if !d.inStream {
		return fmt.Errorf("%w: streamed truncate without stream start", ErrInvalidTransaction)
	}
	relations, bytes, err := d.truncateBudget(d.curTopXid, m.RelationIDs)
	if err != nil {
		return err
	}
	for i, rel := range relations {
		rc := RowChange{
			Op:     OpTruncate,
			Schema: rel.Namespace,
			Table:  rel.RelationName,
			LSN:    walStart.String(),
			XID:    d.curTopXid,
		}
		d.buffer[d.curTopXid] = append(d.buffer[d.curTopXid], bufferedChange{rc: rc, subxid: m.Xid, bytes: bytes[i]})
	}
	return nil
}

func (d *decoder) flushStream(topXid uint32, commitLSN pglogrepl.LSN) []RowChange {
	buffered := d.buffer[topXid]
	d.releaseBuffer(topXid)
	d.inStream = false
	d.curTopXid = 0

	out := make([]RowChange, 0, len(buffered))
	for i := range buffered {
		buffered[i].rc.CommitLSN = commitLSN.String()
		out = append(out, buffered[i].rc)
	}
	return out
}

func (d *decoder) abortStream(topXid, subXid uint32) {
	d.inStream = false
	d.curTopXid = 0
	if topXid == subXid {
		d.releaseBuffer(topXid)
		return
	}

	// PostgreSQL's logical apply worker does not remove only changes whose
	// XID equals subXid. It records the first streamed offset for each
	// subtransaction and truncates the transaction at the aborted subxact's
	// offset. This also discards nested subtransactions (and the remainder of
	// that stream segment); changes after the rollback are sent in a later
	// stream segment. The decoder has the same ordering in its buffer, so the
	// first change for subXid is the equivalent truncation point.
	src := d.buffer[topXid]
	cut := -1
	for i, bc := range src {
		if bc.subxid == subXid {
			cut = i
			break
		}
	}
	if cut < 0 {
		// Empty subtransactions are valid and have no buffered offset. There
		// is nothing to truncate in that case.
		return
	}
	oldChanges := len(src)
	oldBytes := d.usage[topXid]
	d.inflightChanges -= oldChanges
	d.inflightBytes -= oldBytes
	// Clear the discarded tail before reslicing so decoded row maps and their
	// byte slices are no longer retained by the backing array.
	clear(src[cut:])
	src = src[:cut]
	if len(src) == 0 {
		delete(d.buffer, topXid)
		delete(d.usage, topXid)
		return
	}
	d.buffer[topXid] = src
	var bytes int64
	for _, bc := range src {
		bytes += bc.bytes
	}
	d.usage[topXid] = bytes
	d.inflightChanges += len(src)
	d.inflightBytes += bytes
}

func (d *decoder) releaseBuffer(key uint32) {
	buffered, exists := d.buffer[key]
	if !exists {
		return
	}
	d.inflightChanges -= len(buffered)
	d.inflightBytes -= d.usage[key]
	delete(d.buffer, key)
	delete(d.usage, key)
}

func (d *decoder) releaseReservation(key uint32, changes int, bytes int64) {
	d.usage[key] -= bytes
	d.inflightChanges -= changes
	d.inflightBytes -= bytes
	if d.usage[key] == 0 {
		delete(d.usage, key)
	}
}

func (d *decoder) reserveRow(key, relID uint32, oldT, newT *pglogrepl.TupleData) (int64, error) {
	rel, ok := d.rels.get(relID)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrUnknownRelation, relID)
	}
	bytes := estimateChangeBytes(rel, oldT, newT)
	if err := d.reserve(key, 1, bytes); err != nil {
		return 0, err
	}
	return bytes, nil
}

func (d *decoder) truncateBudget(key uint32, relationIDs []uint32) ([]*pglogrepl.RelationMessage, []int64, error) {
	relations := make([]*pglogrepl.RelationMessage, len(relationIDs))
	bytes := make([]int64, len(relationIDs))
	var total int64
	for i, relID := range relationIDs {
		rel, ok := d.rels.get(relID)
		if !ok {
			return nil, nil, fmt.Errorf("%w: %d", ErrUnknownRelation, relID)
		}
		relations[i] = rel
		bytes[i] = estimateRelationChangeBytes(rel)
		total += bytes[i]
	}
	if err := d.reserve(key, len(relationIDs), total); err != nil {
		return nil, nil, err
	}
	return relations, bytes, nil
}

func (d *decoder) reserve(key uint32, changes int, bytes int64) error {
	if changes < 0 || bytes < 0 {
		return fmt.Errorf("%w: invalid estimated transaction size", ErrTransactionLimit)
	}
	currentChanges := len(d.buffer[key])
	if changes > d.limits.maxChanges-currentChanges {
		return fmt.Errorf("%w: changes=%d limit=%d", ErrTransactionLimit, currentChanges+changes, d.limits.maxChanges)
	}
	currentBytes := d.usage[key]
	if bytes > d.limits.maxBytes-currentBytes {
		return fmt.Errorf("%w: bytes=%d limit=%d", ErrTransactionLimit, currentBytes+bytes, d.limits.maxBytes)
	}
	if changes > d.limits.maxInflightChanges-d.inflightChanges {
		return fmt.Errorf("%w: inflight_changes=%d limit=%d", ErrTransactionLimit,
			d.inflightChanges+changes, d.limits.maxInflightChanges)
	}
	if bytes > d.limits.maxInflightBytes-d.inflightBytes {
		return fmt.Errorf("%w: inflight_bytes=%d limit=%d", ErrTransactionLimit,
			d.inflightBytes+bytes, d.limits.maxInflightBytes)
	}
	d.usage[key] = currentBytes + bytes
	d.inflightChanges += changes
	d.inflightBytes += bytes
	return nil
}

const (
	changeEstimateBase int64 = 256
	columnEstimateBase int64 = 64
	stringCopyEstimate int64 = 1
)

func estimateChangeBytes(rel *pglogrepl.RelationMessage, oldT, newT *pglogrepl.TupleData) int64 {
	return changeEstimateBase + int64(len(rel.Namespace)+len(rel.RelationName)) +
		estimateTupleBytes(rel, oldT) + estimateTupleBytes(rel, newT)
}

func estimateRelationChangeBytes(rel *pglogrepl.RelationMessage) int64 {
	return changeEstimateBase + int64(len(rel.Namespace)+len(rel.RelationName))
}

func estimateTupleBytes(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) int64 {
	if tuple == nil {
		return 0
	}
	bytes := columnEstimateBase
	for i, col := range tuple.Columns {
		bytes += columnEstimateBase + int64(len(col.Data))
		if col.DataType != pglogrepl.TupleDataTypeNull && col.DataType != pglogrepl.TupleDataTypeToast {
			// tupleToMap converts values to strings, retaining a second copy.
			bytes += stringCopyEstimate * int64(len(col.Data))
		}
		if i < len(rel.Columns) {
			bytes += int64(len(rel.Columns[i].Name))
		}
	}
	return bytes
}
