// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import sqlapi "github.com/wippyai/runtime/api/service/sql"

// Physical identity belongs to capture, not the portable mutation contract.
// Before/After determine presence; all signed rowids, including zero, are valid.
type capturedMutation struct {
	sqlapi.Mutation
	OldRowID int64
	RowID    int64
}

// coalesceMutations reduces a transaction to its initial/final state at each
// physical row address. A rowid move is a removal and an addition, not an alias:
// the vacated address may be reused in the same transaction. Nil images, never
// numeric sentinels, distinguish absence. First-touch order is deterministic.
func coalesceMutations(pending []capturedMutation) []capturedMutation {
	positions := make(map[mutationKey]int, len(pending))
	result := make([]capturedMutation, 0, len(pending))
	apply := func(change capturedMutation, rowID int64, before, after []any) {
		key := mutationKey{schema: change.Schema, table: change.Table, rowID: rowID}
		if index, ok := positions[key]; ok {
			result[index].After = after
			return
		}
		positions[key] = len(result)
		change.OldRowID, change.RowID = rowID, rowID
		change.Before, change.After = before, after
		result = append(result, change)
	}
	for _, change := range pending {
		switch change.Op {
		case "insert":
			apply(change, change.RowID, nil, change.After)
		case "delete":
			apply(change, change.OldRowID, change.Before, nil)
		case "update":
			if change.OldRowID == change.RowID {
				apply(change, change.RowID, change.Before, change.After)
			} else {
				apply(change, change.OldRowID, change.Before, nil)
				apply(change, change.RowID, nil, change.After)
			}
		}
	}
	out := result[:0]
	for _, change := range result {
		switch {
		case change.Before == nil && change.After == nil:
			continue
		case change.Before == nil:
			change.Op = "insert"
		case change.After == nil:
			change.Op = "delete"
		default:
			change.Op = "update"
		}
		out = append(out, change)
	}
	clear(result[len(out):])
	return out
}
