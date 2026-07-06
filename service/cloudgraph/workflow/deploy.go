// SPDX-License-Identifier: MPL-2.0

// Package workflow holds the deterministic Temporal deploy workflow and its
// activity payload types (docs/design/cloud-graph.md §9.2). The workflow
// walks only stored, ordered arrays: no maps, no time.Now, no goroutines,
// no gonum.
package workflow

import (
	"encoding/json"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Activity names registered on the engine worker.
const (
	ActivityComputePlan      = "cg_compute_plan"
	ActivityExecuteOperation = "cg_execute_operation"
	ActivityFinalizeDeploy   = "cg_finalize_deploy"
)

// Activity tuning (design §9.2: generous StartToClose, liveness via
// heartbeat; §9.4: HeartbeatTimeout must be set explicitly).
const (
	planStartToClose    = 2 * time.Minute
	executeStartToClose = 30 * time.Minute
	executeHeartbeat    = 90 * time.Second
	maxActivityAttempts = 5
)

// ComputePlanResult is the compute_plan activity result carried as JSON.
type ComputePlanResult struct {
	PlanID string     `json:"plan_id"`
	Waves  [][]string `json:"waves"`
}

// DeployResult is the finalize_deploy activity result carried as JSON; the
// workflow returns it verbatim.
type DeployResult struct {
	DeployID     string `json:"deploy_id"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	FinalizedSeq int64  `json:"finalized_seq"`
}

// Deploy is the deterministic deploy workflow: compute_plan, then every wave
// in stored order with per-wave parallel execute_operation activities, then
// finalize_deploy — which always runs, so the deploy record reaches a
// terminal status on every path.
func Deploy(ctx workflow.Context, deployID string) (string, error) {
	planCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: planStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    maxActivityAttempts,
		},
	})

	var failureDetail string

	var planJSON string
	if err := workflow.ExecuteActivity(planCtx, ActivityComputePlan, deployID).Get(ctx, &planJSON); err != nil {
		failureDetail = err.Error()
	}

	var planResult ComputePlanResult
	if failureDetail == "" {
		if err := json.Unmarshal([]byte(planJSON), &planResult); err != nil {
			failureDetail = "undecodable plan result: " + err.Error()
		}
	}

	if failureDetail == "" {
		execCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: executeStartToClose,
			HeartbeatTimeout:    executeHeartbeat,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    time.Second,
				BackoffCoefficient: 2.0,
				MaximumAttempts:    maxActivityAttempts,
			},
		})

	waves:
		for _, wave := range planResult.Waves {
			futures := make([]workflow.Future, len(wave))
			for i, opID := range wave {
				futures[i] = workflow.ExecuteActivity(execCtx, ActivityExecuteOperation, opID)
			}
			for i := range futures {
				var opResult string
				if err := futures[i].Get(ctx, &opResult); err != nil {
					if failureDetail == "" {
						failureDetail = wave[i] + ": " + err.Error()
					}
				}
			}
			if failureDetail != "" {
				break waves
			}
		}
	}

	finalizeCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: planStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    maxActivityAttempts,
		},
	})

	var resultJSON string
	if err := workflow.ExecuteActivity(finalizeCtx, ActivityFinalizeDeploy, deployID, failureDetail).Get(ctx, &resultJSON); err != nil {
		return "", err
	}
	return resultJSON, nil
}
