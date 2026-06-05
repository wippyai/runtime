// SPDX-License-Identifier: MPL-2.0

package postgres

type Op string

const (
	OpInsert   Op = "insert"
	OpUpdate   Op = "update"
	OpDelete   Op = "delete"
	OpSnapshot Op = "snapshot"
	OpTruncate Op = "truncate"
)

type RowChange struct {
	Before    map[string]any `json:"before,omitempty"`
	After     map[string]any `json:"after,omitempty"`
	Op        Op             `json:"op"`
	Schema    string         `json:"schema"`
	Table     string         `json:"table"`
	LSN       string         `json:"lsn"`
	CommitLSN string         `json:"commit_lsn,omitempty"`
	XID       uint32         `json:"xid,omitempty"`
}

func (c RowChange) Relation() string {
	if c.Schema == "" {
		return c.Table
	}
	return c.Schema + "." + c.Table
}
