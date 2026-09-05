// SPDX-License-Identifier: MPL-2.0

package postgres

import "github.com/jackc/pglogrepl"

type relationCache struct {
	rels map[uint32]*pglogrepl.RelationMessage
}

func newRelationCache() *relationCache {
	return &relationCache{rels: make(map[uint32]*pglogrepl.RelationMessage)}
}

func (c *relationCache) put(rel *pglogrepl.RelationMessage) {
	c.rels[rel.RelationID] = rel
}

func (c *relationCache) get(id uint32) (*pglogrepl.RelationMessage, bool) {
	rel, ok := c.rels[id]
	return rel, ok
}

func tupleToMap(rel *pglogrepl.RelationMessage, t *pglogrepl.TupleData) map[string]any {
	if rel == nil || t == nil {
		return nil
	}
	out := make(map[string]any, len(t.Columns))
	for i, col := range t.Columns {
		if i >= len(rel.Columns) {
			break
		}
		name := rel.Columns[i].Name
		switch col.DataType {
		case pglogrepl.TupleDataTypeNull:
			out[name] = nil
		case pglogrepl.TupleDataTypeToast:
			// Not a value. The change carries these names separately.
			continue
		default:
			out[name] = string(col.Data)
		}
	}
	return out
}

func unchangedColumns(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) []string {
	if rel == nil || tuple == nil {
		return nil
	}
	var names []string
	for i, column := range tuple.Columns {
		if i >= len(rel.Columns) {
			break
		}
		if column.DataType == pglogrepl.TupleDataTypeToast {
			names = append(names, rel.Columns[i].Name)
		}
	}
	return names
}
