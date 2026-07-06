// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

type recordedOps struct {
	ids []string
	mu  sync.Mutex
}

func (r *recordedOps) add(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
}

func (r *recordedOps) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

func planJSON(t *testing.T, waves [][]string) string {
	t.Helper()
	out, err := json.Marshal(ComputePlanResult{PlanID: "plan/d1/abc", Waves: waves})
	require.NoError(t, err)
	return string(out)
}

func registerStubs(
	env *testsuite.TestWorkflowEnvironment,
	computePlan func(ctx context.Context, deployID string) (string, error),
	executeOperation func(ctx context.Context, opID string) (string, error),
	finalize func(ctx context.Context, deployID, failureDetail string) (string, error),
) {
	env.RegisterWorkflow(Deploy)
	env.RegisterActivityWithOptions(computePlan, activity.RegisterOptions{Name: ActivityComputePlan})
	env.RegisterActivityWithOptions(executeOperation, activity.RegisterOptions{Name: ActivityExecuteOperation})
	env.RegisterActivityWithOptions(finalize, activity.RegisterOptions{Name: ActivityFinalizeDeploy})
}

func TestDeployWorkflowSuccess(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	ops := &recordedOps{}
	var finalizeDetail string

	registerStubs(env,
		func(_ context.Context, deployID string) (string, error) {
			require.Equal(t, "d1", deployID)
			return planJSON(t, [][]string{{"d1/a", "d1/b"}, {"d1/c"}}), nil
		},
		func(_ context.Context, opID string) (string, error) {
			ops.add(opID)
			return `{"resource_id":"` + opID + `"}`, nil
		},
		func(_ context.Context, deployID, failureDetail string) (string, error) {
			finalizeDetail = failureDetail
			out, err := json.Marshal(DeployResult{DeployID: deployID, Status: "succeeded", FinalizedSeq: 1})
			return string(out), err
		},
	)

	env.ExecuteWorkflow(Deploy, "d1")
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var resultJSON string
	require.NoError(t, env.GetWorkflowResult(&resultJSON))
	var result DeployResult
	require.NoError(t, json.Unmarshal([]byte(resultJSON), &result))
	require.Equal(t, "succeeded", result.Status)
	require.Empty(t, finalizeDetail)

	executed := ops.list()
	require.Len(t, executed, 3)
	require.ElementsMatch(t, []string{"d1/a", "d1/b"}, executed[:2], "wave 0 runs before wave 1")
	require.Equal(t, "d1/c", executed[2])
}

func TestDeployWorkflowMidWaveFailureSkipsRemainingWaves(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	ops := &recordedOps{}
	var finalizeDetail string

	registerStubs(env,
		func(context.Context, string) (string, error) {
			return planJSON(t, [][]string{{"d1/a", "d1/b"}, {"d1/c"}}), nil
		},
		func(_ context.Context, opID string) (string, error) {
			ops.add(opID)
			if opID == "d1/b" {
				return "", temporal.NewNonRetryableApplicationError("operation failed: d1/b: boom", "Internal", nil)
			}
			return `{}`, nil
		},
		func(_ context.Context, deployID, failureDetail string) (string, error) {
			finalizeDetail = failureDetail
			out, err := json.Marshal(DeployResult{DeployID: deployID, Status: "partial", FinalizedSeq: 2, Error: failureDetail})
			return string(out), err
		},
	)

	env.ExecuteWorkflow(Deploy, "d1")
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "a failed deploy still completes the workflow with a terminal status")

	var resultJSON string
	require.NoError(t, env.GetWorkflowResult(&resultJSON))
	var result DeployResult
	require.NoError(t, json.Unmarshal([]byte(resultJSON), &result))
	require.Equal(t, "partial", result.Status)

	require.Contains(t, finalizeDetail, "d1/b")
	require.NotContains(t, ops.list(), "d1/c", "waves after a hard failure must not run")
}

func TestDeployWorkflowPlanFailureStillFinalizes(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	ops := &recordedOps{}
	var finalizeDetail string

	registerStubs(env,
		func(context.Context, string) (string, error) {
			return "", temporal.NewNonRetryableApplicationError("invalid spec: dependency_cycle", "Invalid", nil)
		},
		func(_ context.Context, opID string) (string, error) {
			ops.add(opID)
			return `{}`, nil
		},
		func(_ context.Context, deployID, failureDetail string) (string, error) {
			finalizeDetail = failureDetail
			out, err := json.Marshal(DeployResult{DeployID: deployID, Status: "failed", FinalizedSeq: 3, Error: failureDetail})
			return string(out), err
		},
	)

	env.ExecuteWorkflow(Deploy, "d1")
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	require.Empty(t, ops.list(), "no operations run when planning fails")
	require.Contains(t, finalizeDetail, "dependency_cycle")
}
