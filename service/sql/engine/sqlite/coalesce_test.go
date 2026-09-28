// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// Compare reduction against a separate state-machine model. In particular,
// keys may move, be deleted, and be reused any number of times in one commit.
func TestCoalescePreservesFinalState(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		rng := rand.New(rand.NewSource(seed))
		initial := map[int64][]any{0: {int64(0), "zero"}, 1: {int64(1), "one"}}
		state := cloneRows(initial)
		var pending []capturedMutation
		for step := 0; step < 100; step++ {
			id, target := int64(rng.Intn(9)-4), int64(rng.Intn(9)-4)
			before, exists := state[id]
			change := capturedMutation{Mutation: sqlapi.Mutation{Schema: "main", Table: "items"}, OldRowID: id, RowID: id}
			switch rng.Intn(3) {
			case 0:
				if exists {
					continue
				}
				change.Op, change.After = "insert", []any{id, fmt.Sprint(step)}
				state[id] = change.After
			case 1:
				if !exists {
					continue
				}
				change.Op, change.Before = "delete", before
				delete(state, id)
			case 2:
				if !exists {
					continue
				}
				if _, occupied := state[target]; occupied && target != id {
					continue
				}
				change.Op, change.Before = "update", before
				change.RowID, change.After = target, []any{target, fmt.Sprint(step)}
				delete(state, id)
				state[target] = change.After
			}
			pending = append(pending, change)
		}
		got := cloneRows(initial)
		seen := make(map[int64]bool)
		for _, change := range coalesceMutations(pending) {
			id := change.RowID
			if seen[id] {
				t.Fatalf("seed %d: repeated address %d", seed, id)
			}
			seen[id] = true
			if !reflect.DeepEqual(change.Before, initial[id]) {
				t.Fatalf("seed %d: before image for %d differs from initial state", seed, id)
			}
			if change.After == nil {
				delete(got, id)
			} else {
				got[id] = change.After
			}
		}
		if !reflect.DeepEqual(got, state) {
			t.Fatalf("seed %d: got %v, want %v", seed, got, state)
		}
	}
}

func cloneRows(rows map[int64][]any) map[int64][]any {
	copy := make(map[int64][]any, len(rows))
	for id, row := range rows {
		copy[id] = row
	}
	return copy
}

func TestSavepointIdentifierSemantics(t *testing.T) {
	cases := []struct{ name, want string }{{"Outer", "outer"}, {" a ", " a "}, {"[a]", "[a]"}, {"a;", "a;"}, {"Ä", "Ä"}}
	for _, tc := range cases {
		name, want := tc.name, tc.want
		if got := normalizeSavepointName(name); got != want {
			t.Errorf("%q: got %q, want %q", name, got, want)
		}
	}
}
