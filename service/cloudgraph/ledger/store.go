// SPDX-License-Identifier: MPL-2.0

// Package ledger is the only cloud-graph package touching SQL. It persists
// deploys, plans, operations, signals, and instances with fenced writes
// (docs/design/cloud-graph.md §10, PoC subset).
package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/wippyai/runtime/service/cloudgraph/ledger/schema"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
	schemamanager "github.com/wippyai/runtime/system/schema"
	"go.uber.org/zap"
)

// Store persists cloud-graph state on a SQLite database.
type Store struct {
	db  *sql.DB
	log *zap.Logger
}

// PlanRecord is one persisted plan row; the JSON columns hold ordered arrays
// produced by the planner (design §8.3).
type PlanRecord struct {
	ID         string          `json:"id"`
	DeployID   string          `json:"deploy_id"`
	SpecHash   string          `json:"spec_hash"`
	Operations json.RawMessage `json:"operations"`
	Edges      json.RawMessage `json:"edges"`
	Waves      json.RawMessage `json:"waves"`
}

// NewStore creates a ledger store on the given database handle.
func NewStore(db *sql.DB, log *zap.Logger) *Store {
	if log == nil {
		log = zap.NewNop()
	}
	return &Store{db: db, log: log}
}

// Migrate applies the ledger schema bundle.
func (s *Store) Migrate(ctx context.Context) error {
	manager, err := schemamanager.NewManager(schema.SQLiteBundle(), schemamanager.Target{
		DB:          s.db,
		Dialect:     schemamanager.DialectSQLite,
		LogicalName: schema.BundleName,
	})
	if err != nil {
		return err
	}
	return manager.Setup(ctx)
}

// CreateDeploy idempotently records a deploy and its specs. It reports whether
// this call created the deploy; a repeated deploy_id is not an error
// (design §6.1: Submit is idempotent on DeployID).
func (s *Store) CreateDeploy(ctx context.Context, in resource.DeployInput) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO deploys (id, scope, status, created_at) VALUES (?, ?, ?, ?)",
		in.DeployID, in.Scope, string(resource.DeployPlanning), now())
	if err != nil {
		return false, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, tx.Commit()
	}

	for _, spec := range in.Specs {
		deps, err := json.Marshal(spec.Dependencies)
		if err != nil {
			return false, err
		}
		desired := spec.Desired
		if desired == nil {
			desired = json.RawMessage("{}")
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO resource_specs (deploy_id, resource_id, type, provider_type, desired, dependencies) VALUES (?, ?, ?, ?, ?, ?)",
			in.DeployID, spec.ResourceID, spec.Type, spec.ProviderType, string(desired), string(deps)); err != nil {
			return false, err
		}
	}

	return true, tx.Commit()
}

// GetDeploy loads one deploy record.
func (s *Store) GetDeploy(ctx context.Context, deployID string) (*resource.DeployRecord, error) {
	var rec resource.DeployRecord
	var errMsg sql.NullString
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT id, scope, status, error, finalized_seq FROM deploys WHERE id = ?", deployID).
		Scan(&rec.DeployID, &rec.Scope, &rec.Status, &errMsg, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewDeployNotFoundError(deployID)
	}
	if err != nil {
		return nil, err
	}
	rec.Error = errMsg.String
	rec.FinalizedSeq = seq.Int64
	return &rec, nil
}

// SetDeployStatus performs a fenced status transition.
func (s *Store) SetDeployStatus(ctx context.Context, deployID string, from, to resource.DeployStatus) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE deploys SET status = ? WHERE id = ? AND status = ?",
		string(to), deployID, string(from))
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		cur, err := s.GetDeploy(ctx, deployID)
		if err != nil {
			return err
		}
		if cur.Status == to {
			return nil
		}
		return NewIllegalTransitionError(fmt.Sprintf("deploy %s: %s -> %s (current %s)", deployID, from, to, cur.Status))
	}
	return nil
}

// LoadSpecs loads the declared specs of a deploy ordered by resource_id.
func (s *Store) LoadSpecs(ctx context.Context, deployID string) ([]resource.Spec, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT resource_id, type, provider_type, desired, dependencies FROM resource_specs WHERE deploy_id = ? ORDER BY resource_id",
		deployID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var specs []resource.Spec
	for rows.Next() {
		var spec resource.Spec
		var desired, deps string
		if err := rows.Scan(&spec.ResourceID, &spec.Type, &spec.ProviderType, &desired, &deps); err != nil {
			return nil, err
		}
		spec.Desired = json.RawMessage(desired)
		if err := json.Unmarshal([]byte(deps), &spec.Dependencies); err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, rows.Err()
}

// GetSpec loads one declared spec.
func (s *Store) GetSpec(ctx context.Context, deployID, resourceID string) (*resource.Spec, error) {
	var spec resource.Spec
	var desired, deps string
	err := s.db.QueryRowContext(ctx,
		"SELECT resource_id, type, provider_type, desired, dependencies FROM resource_specs WHERE deploy_id = ? AND resource_id = ?",
		deployID, resourceID).
		Scan(&spec.ResourceID, &spec.Type, &spec.ProviderType, &desired, &deps)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewSpecNotFoundError(deployID, resourceID)
	}
	if err != nil {
		return nil, err
	}
	spec.Desired = json.RawMessage(desired)
	if err := json.Unmarshal([]byte(deps), &spec.Dependencies); err != nil {
		return nil, err
	}
	return &spec, nil
}

// SavePlan idempotently persists a plan with its operations and edges.
// Identifiers are deterministic, so an activity retry re-inserts identical
// rows and OR IGNORE keeps the write replay-safe.
func (s *Store) SavePlan(ctx context.Context, plan *PlanRecord, ops []resource.Operation, edges []resource.Edge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	ts := now()
	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO plans (id, deploy_id, operations, edges, waves, spec_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		plan.ID, plan.DeployID, string(plan.Operations), string(plan.Edges), string(plan.Waves), plan.SpecHash, ts); err != nil {
		return err
	}

	for _, op := range ops {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO operations (id, deploy_id, resource_id, action, status, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			op.ID, op.DeployID, op.ResourceID, string(op.Action), string(resource.OpPlanned), ts); err != nil {
			return err
		}
	}

	for _, edge := range edges {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO dependency_edges (deploy_id, source_resource_id, target_resource_id, kind) VALUES (?, ?, ?, ?)",
			plan.DeployID, edge.SourceResourceID, edge.TargetResourceID, string(edge.Kind)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetPlan loads one plan row.
func (s *Store) GetPlan(ctx context.Context, planID string) (*PlanRecord, error) {
	var rec PlanRecord
	var operations, edges, waves string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, deploy_id, operations, edges, waves, spec_hash FROM plans WHERE id = ?", planID).
		Scan(&rec.ID, &rec.DeployID, &operations, &edges, &waves, &rec.SpecHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewPlanNotFoundError(planID)
	}
	if err != nil {
		return nil, err
	}
	rec.Operations = json.RawMessage(operations)
	rec.Edges = json.RawMessage(edges)
	rec.Waves = json.RawMessage(waves)
	return &rec, nil
}

// GetOperation loads one operation row.
func (s *Store) GetOperation(ctx context.Context, opID string) (*resource.Operation, error) {
	return scanOperation(s.db.QueryRowContext(ctx,
		"SELECT id, deploy_id, resource_id, action, status, current_actor_incarnation, last_signal_seq, actor_pid, error, result FROM operations WHERE id = ?",
		opID), opID)
}

// GetOperationByResource loads the operation of a resource within a deploy.
func (s *Store) GetOperationByResource(ctx context.Context, deployID, resourceID string) (*resource.Operation, error) {
	return scanOperation(s.db.QueryRowContext(ctx,
		"SELECT id, deploy_id, resource_id, action, status, current_actor_incarnation, last_signal_seq, actor_pid, error, result FROM operations WHERE deploy_id = ? AND resource_id = ?",
		deployID, resourceID), deployID+"/"+resourceID)
}

func scanOperation(row *sql.Row, key string) (*resource.Operation, error) {
	var op resource.Operation
	var actorPID, errMsg, result sql.NullString
	err := row.Scan(&op.ID, &op.DeployID, &op.ResourceID, &op.Action, &op.Status,
		&op.Incarnation, &op.LastSignal, &actorPID, &errMsg, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewOperationNotFoundError(key)
	}
	if err != nil {
		return nil, err
	}
	op.ActorPID = actorPID.String
	op.Error = errMsg.String
	if result.Valid {
		op.Result = json.RawMessage(result.String)
	}
	return &op, nil
}

// BumpIncarnation installs a newer actor incarnation via CAS, resetting the
// signal sequence and moving a planned operation to dispatched. A false
// return means a newer incarnation exists or the operation is terminal; the
// caller must exit (design §9.4 step 2).
func (s *Store) BumpIncarnation(ctx context.Context, opID string, incarnation int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE operations
		 SET current_actor_incarnation = ?, last_signal_seq = 0, actor_pid = NULL,
		     status = CASE WHEN status = 'planned' THEN 'dispatched' ELSE status END
		 WHERE id = ? AND current_actor_incarnation < ? AND status IN ('planned','dispatched','in_progress')`,
		incarnation, opID, incarnation)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return false, NewResourceBusyError(opID)
		}
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// SetActorPID records the live actor PID under the incarnation fence.
func (s *Store) SetActorPID(ctx context.Context, opID string, incarnation int64, pidStr string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE operations SET actor_pid = ? WHERE id = ? AND current_actor_incarnation = ?",
		pidStr, opID, incarnation)
	return err
}

// AppendSignal appends a provider signal to the durable mailbox. The write is
// one transaction enforcing the acceptance conditions: incarnation must match
// the operation row, seq must be exactly last_signal_seq+1, and the operation
// must be live. A zero-row CAS rolls back and reports not-accepted
// (design §10.2, conditions C3/C4).
func (s *Store) AppendSignal(ctx context.Context, sig resource.Signal) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE operations SET last_signal_seq = last_signal_seq + 1
		 WHERE id = ? AND current_actor_incarnation = ? AND last_signal_seq = ? AND status IN ('dispatched','in_progress')`,
		sig.OperationID, sig.Incarnation, sig.Seq-1)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, nil
	}

	payload := sig.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	var phase any
	if sig.Phase != "" {
		phase = string(sig.Phase)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO operation_signals (operation_id, incarnation, seq, kind, phase, payload, accepted, created_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?)",
		sig.OperationID, sig.Incarnation, sig.Seq, string(sig.Kind), phase, string(payload), now()); err != nil {
		return false, err
	}

	return true, tx.Commit()
}

// ListSignals loads the accepted signals of one operation incarnation in
// sequence order (recovery replay, design §9.4 step 4).
func (s *Store) ListSignals(ctx context.Context, opID string, incarnation int64) ([]resource.Signal, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT seq, kind, phase, payload FROM operation_signals WHERE operation_id = ? AND incarnation = ? AND accepted = 1 ORDER BY seq",
		opID, incarnation)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var signals []resource.Signal
	for rows.Next() {
		sig := resource.Signal{OperationID: opID, Incarnation: incarnation}
		var phase sql.NullString
		var payload string
		if err := rows.Scan(&sig.Seq, &sig.Kind, &phase, &payload); err != nil {
			return nil, err
		}
		sig.Phase = resource.SignalPhaseName(phase.String)
		sig.Payload = json.RawMessage(payload)
		signals = append(signals, sig)
	}
	return signals, rows.Err()
}

// GetLatestCheckpoint returns the payload of the newest accepted checkpoint
// or output signal across all incarnations, or nil when none exists
// (recovery input, design §9.4 step 4).
func (s *Store) GetLatestCheckpoint(ctx context.Context, opID string) (json.RawMessage, error) {
	var payload string
	err := s.db.QueryRowContext(ctx,
		"SELECT payload FROM operation_signals WHERE operation_id = ? AND accepted = 1 AND kind IN ('checkpoint','output') ORDER BY incarnation DESC, seq DESC LIMIT 1",
		opID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

// MarkInProgress moves a dispatched operation to in_progress under the
// incarnation fence. Already in_progress is not an error.
func (s *Store) MarkInProgress(ctx context.Context, opID string, incarnation int64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE operations SET status = 'in_progress' WHERE id = ? AND current_actor_incarnation = ? AND status = 'dispatched'",
		opID, incarnation)
	return err
}

// CommitOperation is the atomic terminal commit: operation status, result,
// and the resource instance (lifecycle active, bumped generation, published
// outputs) change in one transaction (design I10, I11 subset).
func (s *Store) CommitOperation(ctx context.Context, op *resource.Operation, spec *resource.Spec, scope string, outputs, result json.RawMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if outputs == nil {
		outputs = json.RawMessage("{}")
	}
	if result == nil {
		result = json.RawMessage("{}")
	}

	res, err := tx.ExecContext(ctx,
		"UPDATE operations SET status = 'committed', result = ? WHERE id = ? AND current_actor_incarnation = ? AND status IN ('dispatched','in_progress')",
		string(result), op.ID, op.Incarnation)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return NewCommitFencedError(op.ID)
	}

	desired := spec.Desired
	if desired == nil {
		desired = json.RawMessage("{}")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO resource_instances (resource_id, scope, type, provider_type, lifecycle, generation, desired, outputs, output_epoch, created_by_deploy, updated_at)
		 VALUES (?, ?, ?, ?, 'active', 1, ?, ?, 1, ?, ?)
		 ON CONFLICT(resource_id) DO UPDATE SET
		     lifecycle = 'active',
		     generation = generation + 1,
		     desired = excluded.desired,
		     outputs = excluded.outputs,
		     output_epoch = output_epoch + 1,
		     updated_at = excluded.updated_at`,
		spec.ResourceID, scope, spec.Type, spec.ProviderType, string(desired), string(outputs), op.DeployID, now()); err != nil {
		return err
	}

	return tx.Commit()
}

// Terminalize is the single terminal-transition primitive for failure exits
// (design §9.8): it moves an operation to a terminal status only from the
// given guard set, under the incarnation fence.
func (s *Store) Terminalize(ctx context.Context, opID string, incarnation int64, from []resource.OperationStatus, to resource.OperationStatus, errMsg string) (bool, error) {
	if len(from) == 0 {
		return false, NewIllegalTransitionError("terminalize requires a guard set")
	}

	guard := make([]string, len(from))
	for i, st := range from {
		guard[i] = string(st)
	}

	query, args, err := sq.Update("operations").
		Set("status", string(to)).
		Set("error", errMsg).
		Where(sq.Eq{"id": opID, "current_actor_incarnation": incarnation, "status": guard}).
		ToSql()
	if err != nil {
		return false, err
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// GetInstance loads one resource instance.
func (s *Store) GetInstance(ctx context.Context, resourceID string) (*resource.Instance, error) {
	var inst resource.Instance
	var desired, outputs sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT resource_id, scope, type, provider_type, lifecycle, generation, desired, outputs, output_epoch FROM resource_instances WHERE resource_id = ?",
		resourceID).
		Scan(&inst.ResourceID, &inst.Scope, &inst.Type, &inst.ProviderType, &inst.Lifecycle,
			&inst.Generation, &desired, &outputs, &inst.OutputEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewInstanceNotFoundError(resourceID)
	}
	if err != nil {
		return nil, err
	}
	if outputs.Valid {
		inst.Outputs = json.RawMessage(outputs.String)
	}
	return &inst, nil
}

// ListEdgesFrom loads the outbound dependency edges of a resource within a
// deploy, ordered deterministically.
func (s *Store) ListEdgesFrom(ctx context.Context, deployID, sourceResourceID string) ([]resource.Edge, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT source_resource_id, target_resource_id, kind FROM dependency_edges WHERE deploy_id = ? AND source_resource_id = ? ORDER BY target_resource_id, kind",
		deployID, sourceResourceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var edges []resource.Edge
	for rows.Next() {
		var edge resource.Edge
		if err := rows.Scan(&edge.SourceResourceID, &edge.TargetResourceID, &edge.Kind); err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}
	return edges, rows.Err()
}

// ListOperations loads all operations of a deploy ordered by id.
func (s *Store) ListOperations(ctx context.Context, deployID string) ([]resource.Operation, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, deploy_id, resource_id, action, status, current_actor_incarnation, last_signal_seq, actor_pid, error, result FROM operations WHERE deploy_id = ? ORDER BY id",
		deployID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ops []resource.Operation
	for rows.Next() {
		var op resource.Operation
		var actorPID, errMsg, result sql.NullString
		if err := rows.Scan(&op.ID, &op.DeployID, &op.ResourceID, &op.Action, &op.Status,
			&op.Incarnation, &op.LastSignal, &actorPID, &errMsg, &result); err != nil {
			return nil, err
		}
		op.ActorPID = actorPID.String
		op.Error = errMsg.String
		if result.Valid {
			op.Result = json.RawMessage(result.String)
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// FinalizeDeploy assigns finalized_seq exactly once from the monotonic
// counter and moves the deploy to its terminal status in one transaction
// (design I12 subset). A repeat call returns the persisted result.
func (s *Store) FinalizeDeploy(ctx context.Context, deployID string, status resource.DeployStatus, errMsg string) (int64, error) {
	if !status.Terminal() {
		return 0, NewIllegalTransitionError(fmt.Sprintf("finalize to non-terminal status %s", status))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO counters (name, value) VALUES ('deploys_finalized_seq', 0)"); err != nil {
		return 0, err
	}

	var seq int64
	if err := tx.QueryRowContext(ctx,
		"UPDATE counters SET value = value + 1 WHERE name = 'deploys_finalized_seq' RETURNING value").
		Scan(&seq); err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx,
		"UPDATE deploys SET status = ?, error = ?, finalized_seq = ? WHERE id = ? AND finalized_seq IS NULL AND status IN ('planning','executing')",
		string(status), errMsg, seq, deployID)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		// Re-read through the open transaction: the pool runs SQLite with a
		// single connection, so a second s.db query here would self-deadlock.
		var current string
		var existingSeq sql.NullInt64
		err := tx.QueryRowContext(ctx,
			"SELECT status, finalized_seq FROM deploys WHERE id = ?", deployID).
			Scan(&current, &existingSeq)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, NewDeployNotFoundError(deployID)
		}
		if err != nil {
			return 0, err
		}
		if resource.DeployStatus(current).Terminal() {
			return existingSeq.Int64, nil
		}
		return 0, NewIllegalTransitionError(fmt.Sprintf("deploy %s cannot finalize from %s", deployID, current))
	}

	return seq, tx.Commit()
}

func now() int64 {
	return time.Now().Unix()
}
