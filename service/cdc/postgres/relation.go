// SPDX-License-Identifier: MPL-2.0

package postgres

import "github.com/jackc/pglogrepl"

const unchangedTOAST = "<unchanged-toast>"

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
			out[name] = unchangedTOAST
		default:
			out[name] = string(col.Data)
		}
	}
	return out
}
