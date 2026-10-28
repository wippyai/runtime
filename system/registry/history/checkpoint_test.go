// SPDX-License-Identifier: MPL-2.0

package history_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/sqliteconn"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/postgres"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
	"go.uber.org/zap"
)

// projectionHistory is intentionally a consumer-side contract.  Database
// histories opt in; in-memory histories retain their existing behavior.
type projectionHistory interface {
	registry.History
	registry.ChangeSetReplayer
	registry.HeadCASHistory
	registry.VersionLookup
	ReplayState(context.Context, registry.Version) (*registry.ReplayState, error)
}

type historyFactory struct {
	open func(*testing.T) *historyFixture
	name string
}

type historyFixture struct {
	history             projectionHistory
	db                  *sql.DB
	placeholder         func(int) string
	installWriteFailure func(*testing.T)
	closeHistory        func() error
	reopen              func() (projectionHistory, error)
	migrate             func(context.Context, registry.State) error
	versionsTable       string
	checkpointsTable    string
}

func TestPersistedCheckpointProjectionMatchesFullReplay(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			history := fixture.history
			root, err := history.Head()
			require.NoError(t, err)

			alpha := checkpointEntry("alpha", "first", "one")
			transient := checkpointEntry("transient", "temporary", "two")
			v1 := saveCheckpointVersion(t, history, root, 1, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: alpha}}, true)
			v2 := saveCheckpointVersion(t, history, v1, 2, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: transient}}, true)
			retagged := checkpointEntry("alpha", "second", "three")
			v3 := saveCheckpointVersion(t, history, v2, 3, registry.ChangeSet{{
				Kind: registry.EntryUpdate, Entry: retagged, OriginalEntry: &alpha,
			}}, true)
			v4 := saveCheckpointVersion(t, history, v3, 4, registry.ChangeSet{{
				Kind: registry.EntryDelete, Entry: registry.Entry{ID: transient.ID}, OriginalEntry: &transient,
			}}, true)

			branchAlpha := checkpointEntry("alpha", "branch", "four")
			v5 := saveCheckpointVersion(t, history, v2, 5, registry.ChangeSet{{
				Kind: registry.EntryUpdate, Entry: branchAlpha, OriginalEntry: &alpha,
			}}, false)
			v6 := saveCheckpointVersion(t, history, v5, 6, registry.ChangeSet{{
				Kind: registry.EntryDelete, Entry: registry.Entry{ID: alpha.ID}, OriginalEntry: &branchAlpha,
			}}, false)

			// This tail exceeds the checkpoint policy's version limit.  Each version
			// changes an existing entry, which exercises compact replacement state
			// without relying on a particular checkpoint encoding.
			head := v4
			for id := uint(7); id <= 262; id++ {
				next := checkpointEntry("alpha", "second", fmt.Sprintf("tail-%d", id))
				head = saveCheckpointVersion(t, history, head, id, registry.ChangeSet{{
					Kind: registry.EntryUpdate, Entry: next,
				}}, true)
			}

			for name, target := range map[string]registry.Version{
				"pre_checkpoint": v3,
				"long_tail":      head,
				"branch_delete":  v6,
			} {
				t.Run(name, func(t *testing.T) {
					want := reduceFullReplay(t, history, target)
					got, err := history.ReplayState(t.Context(), target)
					require.NoError(t, err)
					requireReplayStatesEqual(t, want, got)
				})
			}
		})
	}
}

func TestPersistedCheckpointProjectionRetainsBranchStateAcrossSelection(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			history := fixture.history
			root, err := history.Head()
			require.NoError(t, err)
			base := checkpointEntry("entry", "kind-a", "base")
			v1 := saveCheckpointVersion(t, history, root, 1, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: base}}, true)
			left := checkpointEntry("entry", "kind-b", "left")
			v2 := saveCheckpointVersion(t, history, v1, 2, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: left, OriginalEntry: &base}}, true)
			right := checkpointEntry("entry", "kind-c", "right")
			v3 := saveCheckpointVersion(t, history, v1, 3, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: right, OriginalEntry: &base}}, false)

			// Move between the two descendants to cover version selection (undo and
			// redo) independently from the current history head.
			require.NoError(t, history.SetHead(v2))
			require.NoError(t, history.SetHead(v3))
			for _, target := range []registry.Version{v2, v3, v2} {
				want := reduceFullReplay(t, history, target)
				got, err := history.ReplayState(t.Context(), target)
				require.NoError(t, err)
				requireReplayStatesEqual(t, want, got)
			}
		})
	}
}

func TestPersistedCheckpointProjectionMatchesRandomBranchedLineages(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			history := fixture.history
			root, err := history.Head()
			require.NoError(t, err)

			// The seed is fixed so a failing generated lineage is reproducible.
			random := rand.New(rand.NewSource(20261028))
			versions := []registry.Version{root}
			head := root
			// Establish a checkpoint before generating branches. The generated
			// targets below therefore exercise both checkpoint ancestry and tails.
			for id := uint(1); id <= 260; id++ {
				head = saveCheckpointVersion(t, history, head, id, registry.ChangeSet{{
					Kind: registry.EntryUpdate, Entry: checkpointEntry("linear", "checkpoint", fmt.Sprintf("%d", id)),
				}}, true)
				versions = append(versions, head)
			}
			var checkpointCount int
			require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.checkpointsTable).Scan(&checkpointCount))
			require.Positive(t, checkpointCount)
			for id := uint(261); id <= 560; id++ {
				parent := versions[len(versions)-1]
				if len(versions) > 1 && random.Intn(4) == 0 {
					parent = versions[random.Intn(len(versions))]
				}
				name := fmt.Sprintf("entry-%d", random.Intn(9))
				current := checkpointEntry(name, fmt.Sprintf("kind-%d", random.Intn(4)), fmt.Sprintf("value-%d", id))
				original := checkpointEntry(name, fmt.Sprintf("kind-%d", random.Intn(4)), fmt.Sprintf("old-%d", id))
				operation := registry.Operation{Kind: registry.EntryUpdate, Entry: current, OriginalEntry: &original}
				switch random.Intn(3) {
				case 0:
					operation.Kind = registry.EntryCreate
					operation.OriginalEntry = nil
				case 1:
					operation.Kind = registry.EntryDelete
					operation.Entry = registry.Entry{ID: current.ID}
				}
				advanceHead := parent.ID() == head.ID()
				next := saveCheckpointVersion(t, history, parent, id, registry.ChangeSet{operation}, advanceHead)
				versions = append(versions, next)
				if advanceHead {
					head = next
				}
			}

			for index := 1; index < len(versions); index += 29 {
				target := versions[index]
				want := reduceFullReplay(t, history, target)
				got := replayCheckpointState(t, history, target)
				requireReplayStatesEqual(t, want, got)
			}
		})
	}
}

func TestPersistedCheckpointProjectionPreservesDependencyOrdering(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			history := fixture.history
			root, err := history.Head()
			require.NoError(t, err)
			owned := checkpointDependency("worker", "acme/worker", "acme/app", true)
			head := saveCheckpointVersion(t, history, root, 1, registry.ChangeSet{{
				Kind: registry.EntryUpdate, Entry: owned,
			}}, true)
			selection := checkpointDependency("app", "acme/app", "", true)
			head = saveCheckpointVersion(t, history, head, 2, registry.ChangeSet{{
				Kind: registry.EntryCreate, Entry: selection,
			}}, true)
			for id := uint(3); id <= 257; id++ {
				head = saveCheckpointVersion(t, history, head, id, registry.ChangeSet{{
					Kind: registry.EntryUpdate, Entry: checkpointEntry("entry", "checkpoint", fmt.Sprintf("%d", id)),
				}}, true)
			}

			want := reduceFullReplay(t, history, head)
			got := replayCheckpointState(t, history, head)
			requireReplayStatesEqual(t, want, got)
			ownerSelection, ok := got.DependencySelection("acme/app")
			require.True(t, ok)
			require.Equal(t, uint64(2), ownerSelection.Transaction)
			ownedRange, ok := got.Dependencies[owned.ID]
			require.True(t, ok)
			require.Equal(t, uint64(1), ownedRange.First.Transaction)
		})
	}
}

func TestPersistedCheckpointTailPreservesOwnedDependencyOrdering(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			base := seedCheckpointTail(t, fixture.history, 256)
			worker := checkpointDependency("worker", "acme/worker", "acme/app", true)
			changed := worker
			changed.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
			selection := checkpointDependency("app", "acme/app", "", true)
			replacement := worker
			replacement.Kind = registry.EntryKind
			replacement.Data = nil

			cases := []struct {
				name, wantKind          string
				first, second           registry.Operation
				wantTombstone           bool
				operationAfterSelection bool
			}{
				{
					name:     "owner_replaces_earlier_pin",
					first:    registry.Operation{Kind: registry.EntryUpdate, Entry: changed, OriginalEntry: &worker},
					second:   registry.Operation{Kind: registry.EntryCreate, Entry: selection},
					wantKind: changed.Kind,
				},
				{
					name:     "later_pin_survives",
					first:    registry.Operation{Kind: registry.EntryCreate, Entry: selection},
					second:   registry.Operation{Kind: registry.EntryUpdate, Entry: changed, OriginalEntry: &worker},
					wantKind: changed.Kind, operationAfterSelection: true,
				},
				{
					name:          "later_delete_survives",
					first:         registry.Operation{Kind: registry.EntryCreate, Entry: selection},
					second:        registry.Operation{Kind: registry.EntryDelete, Entry: worker},
					wantTombstone: true, operationAfterSelection: true,
				},
				{
					name:     "kind_replacement_survives",
					first:    registry.Operation{Kind: registry.EntryCreate, Entry: selection},
					second:   registry.Operation{Kind: registry.EntryUpdate, Entry: replacement, OriginalEntry: &worker},
					wantKind: replacement.Kind, operationAfterSelection: true,
				},
			}
			for index, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					first := saveCheckpointVersion(t, fixture.history, base, uint(257+index*2), registry.ChangeSet{test.first}, false)
					target := saveCheckpointVersion(t, fixture.history, first, uint(258+index*2), registry.ChangeSet{test.second}, false)
					want := reduceFullReplay(t, fixture.history, target)
					got := replayCheckpointState(t, fixture.history, target)
					requireReplayStatesEqual(t, want, got)
					selected, ok := got.DependencySelection("acme/app")
					require.True(t, ok)
					evidence, ok := got.Dependencies[worker.ID]
					require.True(t, ok)
					if test.operationAfterSelection {
						require.Greater(t, evidence.Last.Transaction, selected.Transaction)
					} else {
						require.Less(t, evidence.Last.Transaction, selected.Transaction)
					}
					require.Equal(t, test.wantTombstone, got.IsTombstoned(worker.ID))
					if test.wantKind != "" {
						require.Equal(t, test.wantKind, got.Entries[worker.ID].Kind)
					}
				})
			}
		})
	}
}

func TestPersistedReplayPreparesActivation(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		for _, mode := range []string{"uncached", "unsupported", "cached", "short_tail", "stale_head", "write_failure", "resolution", "resolution_write_failure", "checkpoint_tail", "cancelled"} {
			t.Run(factory.name+"/"+mode, func(t *testing.T) {
				fixture := factory.open(t)
				count := uint(256)
				switch mode {
				case "short_tail":
					count = 3
				case "checkpoint_tail":
					count = 512
				}
				target := seedCheckpointTail(t, fixture.history, count)
				if mode != "cached" && mode != "checkpoint_tail" {
					_, err := fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.checkpointsTable+" SET format_version=999")
					require.NoError(t, err)
					if mode != "unsupported" {
						_, err = fixture.db.ExecContext(t.Context(), "DELETE FROM "+fixture.checkpointsTable)
						require.NoError(t, err)
					}
				}
				if mode != "cached" && mode != "checkpoint_tail" {
					_, err := fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.versionsTable+" SET lineage_digest=NULL")
					require.NoError(t, err)
				}
				if mode == "checkpoint_tail" {
					_, err := fixture.db.ExecContext(t.Context(), "DELETE FROM "+fixture.checkpointsTable+" WHERE version_id="+fixture.placeholder(1), target.ID())
					require.NoError(t, err)
				}
				preparer, ok := fixture.history.(registry.ReplayActivationHistory)
				require.True(t, ok, "durable history prepares projection and activation in one replay")
				want := reduceFullReplay(t, fixture.history, target)
				root, err := fixture.history.GetVersion(0)
				require.NoError(t, err)
				expected := target
				if mode != "stale_head" {
					require.NoError(t, fixture.history.SetHead(root))
					expected = root
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				state, activate, err := preparer.PrepareReplayState(ctx, target)
				require.NoError(t, err)
				requireReplayStatesEqual(t, want, state)
				switch mode {
				case "stale_head":
					require.NoError(t, fixture.history.SetHead(root))
				case "write_failure", "resolution_write_failure":
					fixture.installWriteFailure(t)
				}
				var resolution *registry.DependencyResolution
				if mode == "resolution" || mode == "resolution_write_failure" {
					resolution = (&registry.DependencyResolution{InputDigest: "roots"}).Canonical()
				}
				if mode == "cancelled" {
					cancel()
				}
				err = activate(expected, resolution)
				head, headErr := fixture.history.Head()
				require.NoError(t, headErr)
				if mode == "stale_head" || mode == "write_failure" || mode == "resolution_write_failure" || mode == "cancelled" {
					require.Equal(t, root.ID(), head.ID())
					require.Error(t, err)
					var checkpoints int
					require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.checkpointsTable).Scan(&checkpoints))
					require.Zero(t, checkpoints)
					var chain sql.NullString
					require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT lineage_digest FROM "+fixture.versionsTable+" WHERE id="+fixture.placeholder(1), target.ID()).Scan(&chain))
					require.False(t, chain.Valid, "failed activation does not publish a lineage binding")
					if resolution != nil {
						_, err := fixture.history.(registry.ResolutionHistory).GetDependencyResolution(target)
						require.ErrorIs(t, err, registry.ErrDependencyResolutionNotFound)
					}
					return
				}
				require.NoError(t, err)
				require.Equal(t, target.ID(), head.ID())
				if resolution != nil {
					stored, err := fixture.history.(registry.ResolutionHistory).GetDependencyResolution(target)
					require.NoError(t, err)
					require.Equal(t, resolution, stored)
				}
				requireReplayStatesEqual(t, want, replayCheckpointState(t, fixture.history, target))
				var checkpoints int
				require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.checkpointsTable+" WHERE format_version=1").Scan(&checkpoints))
				switch mode {
				case "short_tail":
					require.Zero(t, checkpoints)
				case "checkpoint_tail":
					require.Equal(t, 2, checkpoints)
				default:
					require.Equal(t, 1, checkpoints)
				}
			})
		}
	}
}

func TestPersistedCheckpointWritesAtReplayBudget(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			head := seedCheckpointTail(t, fixture.history, 256)
			var format int
			err := fixture.db.QueryRowContext(t.Context(), "SELECT format_version FROM "+fixture.checkpointsTable+" WHERE version_id = "+fixture.placeholder(1), head.ID()).Scan(&format)
			require.NoError(t, err)
			require.Equal(t, 1, format)
			requireReplayStatesEqual(t, reduceFullReplay(t, fixture.history, head), replayCheckpointState(t, fixture.history, head))
		})
	}
}

func TestPersistedCheckpointRejectsCorruptSupportedCache(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		for field, expectedError := range map[string]string{"digest": "digest mismatch", "lineage_digest": "lineage mismatch"} {
			t.Run(factory.name+"/"+field, func(t *testing.T) {
				fixture := factory.open(t)
				head := seedCheckpointTail(t, fixture.history, 256)
				_, err := fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.checkpointsTable+" SET "+field+" = "+fixture.placeholder(1)+" WHERE version_id = "+fixture.placeholder(2), "invalid", head.ID())
				require.NoError(t, err)
				_, err = fixture.history.ReplayState(t.Context(), head)
				require.Error(t, err)
				require.ErrorContains(t, err, expectedError)
				_, _, err = fixture.history.(registry.ReplayActivationHistory).PrepareReplayState(t.Context(), head)
				require.ErrorContains(t, err, expectedError)
			})
		}
	}
}

func TestPersistedCheckpointUnsupportedFormatRebuildsOnNextSave(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			head := seedCheckpointTail(t, fixture.history, 256)
			_, err := fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.checkpointsTable+" SET format_version = "+fixture.placeholder(1)+" WHERE version_id = "+fixture.placeholder(2), 999, head.ID())
			require.NoError(t, err)
			requireReplayStatesEqual(t, reduceFullReplay(t, fixture.history, head), replayCheckpointState(t, fixture.history, head))
			require.NoError(t, fixture.history.CompareAndSetHead(head, head))
			var format int
			err = fixture.db.QueryRowContext(t.Context(), "SELECT format_version FROM "+fixture.checkpointsTable+" WHERE version_id = "+fixture.placeholder(1), head.ID()).Scan(&format)
			require.NoError(t, err)
			require.Equal(t, 1, format)

			_, err = fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.checkpointsTable+" SET format_version = "+fixture.placeholder(1)+" WHERE version_id = "+fixture.placeholder(2), 999, head.ID())
			require.NoError(t, err)
			next := saveCheckpointVersion(t, fixture.history, head, 257, registry.ChangeSet{{
				Kind: registry.EntryUpdate, Entry: checkpointEntry("entry", "checkpoint", "257"),
			}}, true)
			err = fixture.db.QueryRowContext(t.Context(), "SELECT format_version FROM "+fixture.checkpointsTable+" WHERE version_id = "+fixture.placeholder(1), next.ID()).Scan(&format)
			require.NoError(t, err)
			require.Equal(t, 1, format)
			requireReplayStatesEqual(t, reduceFullReplay(t, fixture.history, next), replayCheckpointState(t, fixture.history, next))
		})
	}
}

func TestPersistedCheckpointSaveIsAtomicWithCheckpointWriteAndHeadCAS(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Run("checkpoint_write", func(t *testing.T) {
				fixture := factory.open(t)
				before := seedCheckpointTail(t, fixture.history, 255)
				fixture.installWriteFailure(t)
				failed := version.FromParent(before, 256)
				err := fixture.history.Save(failed, registry.ChangeSet{{
					Kind: registry.EntryUpdate, Entry: checkpointEntry("entry", "checkpoint", "failed"),
				}}, true)
				require.ErrorContains(t, err, "checkpoint write rejected")
				assertVersionAbsentAndHeadUnchanged(t, fixture, failed, before)
			})

			t.Run("head_cas", func(t *testing.T) {
				fixture := factory.open(t)
				before := seedCheckpointTail(t, fixture.history, 255)
				staleHead, err := fixture.history.GetVersion(254)
				require.NoError(t, err)
				require.NoError(t, fixture.history.SetHead(staleHead))
				failed := version.FromParent(before, 256)
				err = fixture.history.Save(failed, registry.ChangeSet{{
					Kind: registry.EntryUpdate, Entry: checkpointEntry("entry", "checkpoint", "stale"),
				}}, true)
				require.Error(t, err)
				assertVersionAbsentAndHeadUnchanged(t, fixture, failed, staleHead)
			})
		})
	}
}

func TestPersistedCheckpointRebuildFailureRollsBackHeadCAS(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			target := seedCheckpointTail(t, fixture.history, 256)
			expected, err := fixture.history.GetVersion(255)
			require.NoError(t, err)
			require.NoError(t, fixture.history.SetHead(expected))
			_, err = fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.checkpointsTable+" SET format_version = "+fixture.placeholder(1)+" WHERE version_id = "+fixture.placeholder(2), 999, target.ID())
			require.NoError(t, err)
			beforeBindings := replayLineageBindings(t, fixture)
			fixture.installWriteFailure(t)

			err = fixture.history.CompareAndSetHead(expected, target)
			require.ErrorContains(t, err, "checkpoint write rejected")
			head, err := fixture.history.Head()
			require.NoError(t, err)
			require.Equal(t, expected.ID(), head.ID())
			var format int
			require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT format_version FROM "+fixture.checkpointsTable+" WHERE version_id = "+fixture.placeholder(1), target.ID()).Scan(&format))
			require.Equal(t, 999, format)
			require.Equal(t, beforeBindings, replayLineageBindings(t, fixture))
		})
	}
}

func TestPersistedCheckpointSchemaUpgradeFromV11PreservesHistory(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			root, err := fixture.history.Head()
			require.NoError(t, err)
			entry := checkpointEntry("upgrade", "checkpoint", "preserved")
			target := saveCheckpointVersion(t, fixture.history, root, 1, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}}, true)
			require.NoError(t, fixture.closeHistory())

			_, err = fixture.db.ExecContext(t.Context(), "DROP TABLE "+fixture.checkpointsTable)
			require.NoError(t, err)
			_, err = fixture.db.ExecContext(t.Context(), "ALTER TABLE "+fixture.versionsTable+" DROP COLUMN lineage_digest")
			require.NoError(t, err)
			_, err = fixture.db.ExecContext(t.Context(), "UPDATE "+fixture.schemaVersionTable()+" SET curr_version = "+fixture.placeholder(1)+" WHERE name = "+fixture.placeholder(2), "1.1", "registry_history")
			require.NoError(t, err)

			upgraded, err := fixture.reopen()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, upgraded.(interface{ Close() error }).Close()) })
			changes, err := upgraded.Get(target)
			require.NoError(t, err)
			require.Len(t, changes, 1)
			require.Equal(t, registry.EntryCreate, changes[0].Kind)
			require.True(t, entry.ID.Equal(changes[0].Entry.ID))
			require.Equal(t, entry.Kind, changes[0].Entry.Kind)
			require.Equal(t, entry.Data, changes[0].Entry.Data)
			head, err := upgraded.Head()
			require.NoError(t, err)
			require.Equal(t, target.ID(), head.ID())
			var schemaVersion string
			err = fixture.db.QueryRowContext(t.Context(), "SELECT curr_version FROM "+fixture.schemaVersionTable()+" WHERE name = "+fixture.placeholder(1), "registry_history").Scan(&schemaVersion)
			require.NoError(t, err)
			require.Equal(t, "1.2", schemaVersion)
		})
	}
}

func TestPersistedCheckpointInvalidatesAfterHistoryRewrite(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			head := seedCheckpointTail(t, fixture.history, 256)
			var before int
			err := fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.checkpointsTable).Scan(&before)
			require.NoError(t, err)
			require.Positive(t, before)

			baseline := registry.State{{
				ID:       registry.NewID("checkpoint", "entry"),
				Registry: registry.EntryMetadata{Owner: "acme/app"},
			}}
			require.NoError(t, fixture.migrate(t.Context(), baseline))
			var checkpointCount, bindingCount int
			require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.checkpointsTable).Scan(&checkpointCount))
			require.Zero(t, checkpointCount)
			require.NoError(t, fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.versionsTable+" WHERE lineage_digest IS NOT NULL").Scan(&bindingCount))
			require.Zero(t, bindingCount)
			requireReplayStatesEqual(t, reduceFullReplay(t, fixture.history, head), replayCheckpointState(t, fixture.history, head))
		})
	}
}

func (f *historyFixture) schemaVersionTable() string { return f.table("schema_version") }

func (f *historyFixture) table(name string) string {
	if f.versionsTable == "versions" {
		return name
	}
	return strings.TrimSuffix(f.versionsTable, `"versions"`) + `"` + name + `"`
}

func seedCheckpointTail(t *testing.T, history registry.History, count uint) registry.Version {
	t.Helper()
	head, err := history.Head()
	require.NoError(t, err)
	for id := uint(1); id <= count; id++ {
		head = saveCheckpointVersion(t, history, head, id, registry.ChangeSet{{
			Kind: registry.EntryUpdate, Entry: checkpointEntry("entry", "checkpoint", fmt.Sprintf("%d", id)),
		}}, true)
	}
	return head
}

func replayCheckpointState(t *testing.T, history projectionHistory, target registry.Version) *registry.ReplayState {
	t.Helper()
	state, err := history.ReplayState(t.Context(), target)
	require.NoError(t, err)
	prepared, _, err := history.(registry.ReplayActivationHistory).PrepareReplayState(t.Context(), target)
	require.NoError(t, err)
	requireReplayStatesEqual(t, state, prepared)
	return state
}

func assertVersionAbsentAndHeadUnchanged(t *testing.T, fixture *historyFixture, absent, expectedHead registry.Version) {
	t.Helper()
	var count int
	err := fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.versionsTable+" WHERE id = "+fixture.placeholder(1), absent.ID()).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
	head, err := fixture.history.Head()
	require.NoError(t, err)
	require.Equal(t, expectedHead.ID(), head.ID())
	var checkpointCount int
	err = fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+fixture.checkpointsTable+" WHERE version_id = "+fixture.placeholder(1), absent.ID()).Scan(&checkpointCount)
	require.NoError(t, err)
	require.Zero(t, checkpointCount)
}

func replayLineageBindings(t *testing.T, fixture *historyFixture) map[uint]string {
	t.Helper()
	rows, err := fixture.db.QueryContext(t.Context(), "SELECT id, COALESCE(lineage_digest, '') FROM "+fixture.versionsTable+" ORDER BY id")
	require.NoError(t, err)
	result := make(map[uint]string)
	for rows.Next() {
		var id uint
		var digest string
		require.NoError(t, rows.Scan(&id, &digest))
		result[id] = digest
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	return result
}

func reduceFullReplay(t *testing.T, history registry.ChangeSetReplayer, target registry.Version) *registry.ReplayState {
	t.Helper()
	state := registry.NewReplayState()
	require.NoError(t, history.ReplayChanges(t.Context(), target, func(changes registry.ChangeSet) error {
		return state.Apply(t.Context(), changes)
	}))
	return state
}

func requireReplayStatesEqual(t *testing.T, want, got *registry.ReplayState) {
	t.Helper()
	require.Equal(t, normalizedReplayState(want), normalizedReplayState(got))
}

func normalizedReplayState(state *registry.ReplayState) *registry.ReplayState {
	if state == nil {
		return nil
	}
	copyOperation := func(operation registry.Operation) registry.Operation {
		operation.Entry.ID = operation.Entry.ID.Canonical()
		if operation.OriginalEntry != nil {
			original := *operation.OriginalEntry
			original.ID = original.ID.Canonical()
			operation.OriginalEntry = &original
		}
		return operation
	}
	copyRecorded := func(recorded registry.ReplayOperation) registry.ReplayOperation {
		recorded.Operation = copyOperation(recorded.Operation)
		return recorded
	}
	copyRange := func(evidence registry.ReplayOperationRange) registry.ReplayOperationRange {
		evidence.First = copyRecorded(evidence.First)
		evidence.Last = copyRecorded(evidence.Last)
		return evidence
	}
	normalized := registry.NewReplayState()
	normalized.Transaction = state.Transaction
	normalized.Sequence = state.Sequence
	for id, entry := range state.Entries {
		id = id.Canonical()
		entry.ID = entry.ID.Canonical()
		normalized.Entries[id] = entry
	}
	for id := range state.Tombstones {
		normalized.Tombstones[id.Canonical()] = struct{}{}
	}
	for id, evidence := range state.Dependencies {
		normalized.Dependencies[id.Canonical()] = copyRange(evidence)
	}
	for id, evidence := range state.AmbiguousDeletes {
		normalized.AmbiguousDeletes[id.Canonical()] = copyRange(evidence)
	}
	for component, recorded := range state.Selections {
		normalized.Selections[component] = copyRecorded(recorded)
	}
	return normalized
}

func checkpointEntry(name, kind, value string) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("checkpoint", name),
		Kind: kind,
		Data: payload.NewString(value),
	}
}

func checkpointDependency(name, component, owner string, root bool) registry.Entry {
	return registry.Entry{
		ID:       registry.NewID("checkpoint.deps", name),
		Kind:     registry.NamespaceDependency,
		Data:     payload.New(map[string]any{"component": component, "version": "*"}),
		Registry: registry.EntryMetadata{Owner: owner, Root: root},
	}
}

func saveCheckpointVersion(t *testing.T, history registry.History, parent registry.Version, id uint, changes registry.ChangeSet, head bool) registry.Version {
	t.Helper()
	v := version.FromParent(parent, id)
	require.NoError(t, history.Save(v, changes, head))
	return v
}

func persistedHistoryFactories() []historyFactory {
	return []historyFactory{
		{
			name: "sqlite",
			open: func(t *testing.T) *historyFixture {
				t.Helper()
				path := filepath.Join(t.TempDir(), "history.db")
				history, err := sqlite.NewSQLite(path, zap.NewNop())
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, history.Close()) })
				db := sqliteconn.Open("file:" + path + "?_busy_timeout=5000&_foreign_keys=on")
				t.Cleanup(func() { require.NoError(t, db.Close()) })
				return &historyFixture{
					history:          history,
					db:               db,
					versionsTable:    "versions",
					checkpointsTable: "replay_checkpoints",
					placeholder:      func(int) string { return "?" },
					closeHistory:     history.Close,
					reopen: func() (projectionHistory, error) {
						return sqlite.NewSQLite(path, zap.NewNop())
					},
					migrate: func(ctx context.Context, baseline registry.State) error {
						return sqlite.MigrateEntryMetadata(ctx, history, baseline)
					},
					installWriteFailure: func(t *testing.T) {
						t.Helper()
						_, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_replay_checkpoint
							BEFORE INSERT ON replay_checkpoints BEGIN SELECT RAISE(ABORT, 'checkpoint write rejected'); END`)
						require.NoError(t, err)
					},
				}
			},
		},
		{
			name: "postgres",
			open: func(t *testing.T) *historyFixture {
				t.Helper()
				dsn := strings.TrimSpace(os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN"))
				if dsn == "" {
					t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
				}
				schema := fmt.Sprintf("wippy_registry_checkpoint_contract_%d", os.Getpid())
				db, err := sql.Open("postgres", dsn)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				_, err = db.ExecContext(t.Context(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
				require.NoError(t, err)
				t.Cleanup(func() {
					_, dropErr := db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
					require.NoError(t, dropErr)
				})
				history, err := postgres.NewPostgres(dsn, schema, zap.NewNop())
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, history.Close()) })
				qualified := func(table string) string { return fmt.Sprintf("%q.%q", schema, table) }
				return &historyFixture{
					history:          history,
					db:               db,
					versionsTable:    qualified("versions"),
					checkpointsTable: qualified("replay_checkpoints"),
					placeholder:      func(index int) string { return fmt.Sprintf("$%d", index) },
					closeHistory:     history.Close,
					reopen: func() (projectionHistory, error) {
						return postgres.NewPostgres(dsn, schema, zap.NewNop())
					},
					migrate: func(ctx context.Context, baseline registry.State) error {
						return postgres.MigrateEntryMetadata(ctx, history, baseline)
					},
					installWriteFailure: func(t *testing.T) {
						t.Helper()
						_, err := db.ExecContext(t.Context(), fmt.Sprintf(`CREATE FUNCTION %q.reject_replay_checkpoint() RETURNS trigger
							LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'checkpoint write rejected'; END; $$`, schema))
						require.NoError(t, err)
						_, err = db.ExecContext(t.Context(), fmt.Sprintf(`CREATE TRIGGER reject_replay_checkpoint
							BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %q.reject_replay_checkpoint()`, qualified("replay_checkpoints"), schema))
						require.NoError(t, err)
					},
				}
			},
		},
	}
}

func TestPersistedReplayValidatesMissingChangesetsBeforeCallbacks(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			target := seedCheckpointTail(t, fixture.history, 3)
			_, err := fixture.db.ExecContext(t.Context(), "DELETE FROM "+fixture.table("changesets")+" WHERE version_id="+fixture.placeholder(1), target.ID())
			require.NoError(t, err)
			callbacks := 0
			err = fixture.history.ReplayChanges(t.Context(), target, func(registry.ChangeSet) error { callbacks++; return nil })
			require.Error(t, err)
			require.Zero(t, callbacks, "invalid lineage must fail before any transaction is applied")
		})
	}
}

func TestPersistedReplayReadsBoundedPagesBeforeCallbacks(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			const count = 3000
			tx, err := fixture.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			for id := 1; id <= count; id++ {
				_, err = tx.ExecContext(t.Context(), "INSERT INTO "+fixture.versionsTable+" (id,parent_id) VALUES ("+fixture.placeholder(1)+","+fixture.placeholder(2)+")", id, id-1)
				require.NoError(t, err)
				_, err = tx.ExecContext(t.Context(), "INSERT INTO "+fixture.table("changesets")+" (version_id,data) VALUES ("+fixture.placeholder(1)+","+fixture.placeholder(2)+")", id, []byte{0x90})
				require.NoError(t, err)
			}
			require.NoError(t, tx.Commit())
			target, err := fixture.history.GetVersion(count)
			require.NoError(t, err)
			callbacks := 0
			err = fixture.history.ReplayChanges(t.Context(), target, func(cs registry.ChangeSet) error {
				require.Empty(t, cs)
				callbacks++
				// Both reads and writes must be safe with SQLite's single connection.
				if _, err := fixture.history.Head(); err != nil {
					return err
				}
				if callbacks == 1 {
					_, err := fixture.db.ExecContext(t.Context(), "DELETE FROM "+fixture.table("changesets")+" WHERE version_id IN (2,"+fixture.placeholder(1)+")", count)
					return err
				}
				return nil
			})
			require.ErrorContains(t, err, "changeset not found for version")
			require.Greater(t, callbacks, 1, "the first page includes the next transaction")
			require.Less(t, callbacks, count-1, "the missing tail fails when its page is read")
		})
	}
}

func TestPersistedReplayCallbackCanSaveBranch(t *testing.T) {
	for _, factory := range persistedHistoryFactories() {
		t.Run(factory.name, func(t *testing.T) {
			fixture := factory.open(t)
			target := seedCheckpointTail(t, fixture.history, 3)
			root, err := fixture.history.GetVersion(registry.RootVersion)
			require.NoError(t, err)
			branch := version.FromParent(root, 1000)
			var retained []registry.ChangeSet
			err = fixture.history.ReplayChanges(t.Context(), target, func(cs registry.ChangeSet) error {
				retained = append(retained, cs)
				if len(retained) == 1 {
					return fixture.history.Save(branch, cs, false)
				}
				return nil
			})
			require.NoError(t, err)
			require.Len(t, retained, 3)
			for i, changes := range retained {
				original, err := fixture.history.Get(version.New(uint(i + 1)))
				require.NoError(t, err)
				require.Equal(t, original, changes, "callbacks retain independent original transactions")
			}
			head, err := fixture.history.Head()
			require.NoError(t, err)
			require.Equal(t, target.ID(), head.ID())
		})
	}
}
