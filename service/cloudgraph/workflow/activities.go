// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"context"
	"encoding/json"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/service/cloudgraph/attempt"
	"github.com/wippyai/runtime/service/cloudgraph/ledger"
	"github.com/wippyai/runtime/service/cloudgraph/plan"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// Activities holds the Go activity handlers published to the function
// registry and registered on the engine worker (design §9.1).
type Activities struct {
	Store      *ledger.Store
	Executor   *attempt.Executor
	Transcoder payload.Transcoder
	Log        *zap.Logger
}

// ComputePlan loads the deploy's specs, computes the deterministic plan,
// persists it, and moves the deploy to executing (design §8.3).
func (a *Activities) ComputePlan(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	deployID, err := a.stringArg(task, 0)
	if err != nil {
		return nil, err
	}

	specs, err := a.Store.LoadSpecs(ctx, deployID)
	if err != nil {
		return nil, err
	}

	doc, err := plan.Compute(deployID, specs)
	if err != nil {
		return nil, err
	}

	operations := make([]resource.Operation, len(doc.Operations))
	for i, op := range doc.Operations {
		operations[i] = resource.Operation{
			ID:         op.OpID,
			DeployID:   deployID,
			ResourceID: op.ResourceID,
			Action:     op.Action,
		}
	}
	edges := make([]resource.Edge, len(doc.Edges))
	for i, edge := range doc.Edges {
		edges[i] = resource.Edge{
			SourceResourceID: edge.Source,
			TargetResourceID: edge.Target,
			Kind:             edge.Kind,
		}
	}

	opsJSON, err := json.Marshal(doc.Operations)
	if err != nil {
		return nil, err
	}
	edgesJSON, err := json.Marshal(doc.Edges)
	if err != nil {
		return nil, err
	}
	wavesJSON, err := json.Marshal(doc.Waves)
	if err != nil {
		return nil, err
	}

	if err := a.Store.SavePlan(ctx, &ledger.PlanRecord{
		ID:         doc.PlanID,
		DeployID:   deployID,
		Operations: opsJSON,
		Edges:      edgesJSON,
		Waves:      wavesJSON,
		SpecHash:   doc.SpecHash,
	}, operations, edges); err != nil {
		return nil, err
	}

	if err := a.Store.SetDeployStatus(ctx, deployID, resource.DeployPlanning, resource.DeployExecuting); err != nil {
		return nil, err
	}

	out, err := json.Marshal(ComputePlanResult{PlanID: doc.PlanID, Waves: doc.Waves})
	if err != nil {
		return nil, err
	}
	return &runtime.Result{Value: payload.New(string(out))}, nil
}

// ExecuteOperation runs one operation through the executor, threading the
// activity attempt as the actor incarnation and heartbeating through the raw
// activity context (design §9.4).
func (a *Activities) ExecuteOperation(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	opID, err := a.stringArg(task, 0)
	if err != nil {
		return nil, err
	}

	req := attempt.Request{OpID: opID, Incarnation: 1}
	if actx := rawActivityContext(ctx); actx != nil {
		req.Incarnation = int64(activity.GetInfo(actx).Attempt)
		req.Heartbeat = func(details ...any) {
			activity.RecordHeartbeat(actx, details...)
		}
	}

	result, err := a.Executor.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	return &runtime.Result{Value: payload.New(string(result))}, nil
}

// FinalizeDeploy terminalizes abandoned operations, derives the terminal
// deploy status, and assigns finalized_seq exactly once (design §9.3, I12).
func (a *Activities) FinalizeDeploy(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	deployID, err := a.stringArg(task, 0)
	if err != nil {
		return nil, err
	}
	failureDetail, err := a.stringArg(task, 1)
	if err != nil {
		return nil, err
	}

	ops, err := a.Store.ListOperations(ctx, deployID)
	if err != nil {
		return nil, err
	}

	committed, failed := 0, 0
	for _, op := range ops {
		switch op.Status {
		case resource.OpCommitted:
			committed++
		case resource.OpFailed:
			failed++
		default:
			done, err := a.Store.Terminalize(ctx, op.ID, op.Incarnation,
				[]resource.OperationStatus{resource.OpPlanned, resource.OpDispatched, resource.OpInProgress},
				resource.OpFailed, "abandoned at deploy finalize")
			if err != nil {
				return nil, err
			}
			if done {
				failed++
			} else {
				current, err := a.Store.GetOperation(ctx, op.ID)
				if err != nil {
					return nil, err
				}
				if current.Status == resource.OpCommitted {
					committed++
				} else {
					failed++
				}
			}
		}
	}

	status := resource.DeployFailed
	switch {
	case len(ops) > 0 && failed == 0 && committed == len(ops):
		status = resource.DeploySucceeded
	case committed > 0:
		status = resource.DeployPartial
	}
	if status == resource.DeployFailed && failureDetail == "" {
		failureDetail = "no operations committed"
	}
	if status == resource.DeploySucceeded {
		failureDetail = ""
	}

	seq, err := a.Store.FinalizeDeploy(ctx, deployID, status, failureDetail)
	if err != nil {
		return nil, err
	}

	if rec, err := a.Store.GetDeploy(ctx, deployID); err == nil && rec.Status.Terminal() {
		status = rec.Status
		failureDetail = rec.Error
	}

	out, err := json.Marshal(DeployResult{
		DeployID:     deployID,
		Status:       string(status),
		FinalizedSeq: seq,
		Error:        failureDetail,
	})
	if err != nil {
		return nil, err
	}
	return &runtime.Result{Value: payload.New(string(out))}, nil
}

// stringArg decodes one positional string payload of the task.
func (a *Activities) stringArg(task runtime.Task, idx int) (string, error) {
	if idx >= len(task.Payloads) {
		return "", apierror.New(apierror.Invalid, "missing activity argument").WithRetryable(apierror.False)
	}
	var value string
	if err := a.Transcoder.Unmarshal(task.Payloads[idx], &value); err != nil {
		return "", apierror.New(apierror.Invalid, "undecodable activity argument: "+err.Error()).WithRetryable(apierror.False)
	}
	return value, nil
}

// rawActivityContext extracts the raw Temporal activity context threaded by
// the worker through the frame context (design §9.4 step 2).
func rawActivityContext(ctx context.Context) context.Context {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	val, ok := fc.Get(temporalapi.ActivityContextKey())
	if !ok {
		return nil
	}
	actx, ok := val.(context.Context)
	if !ok {
		return nil
	}
	return actx
}
