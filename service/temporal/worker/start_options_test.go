package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/process"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

func TestApplyTemporalStartWorkflowOptions_AllMappedFields(t *testing.T) {
	typedSearchAttributes := temporal.NewSearchAttributes(
		temporal.NewSearchAttributeKeyKeyword("CustomKeywordField").ValueSet("tenant-a"),
	)

	startOptions := attrs.NewBag()
	startOptions.Set(optionWorkflowID, "orders:workflow-1")
	startOptions.Set(optionWorkflowTaskQueue, "orders-queue")
	startOptions.Set(optionWorkflowExecutionTimeout, "5m")
	startOptions.Set(optionWorkflowRunTimeout, int64(60000))
	startOptions.Set(optionWorkflowTaskTimeout, uint64(5000))
	startOptions.Set(optionWorkflowIDConflictPolicy, "use_existing")
	startOptions.Set(optionWorkflowIDReusePolicy, "allow_duplicate_failed_only")
	startOptions.Set(optionWorkflowExecutionErrorWhenAlreadyStarted, false)
	startOptions.Set(optionWorkflowRetryPolicy, map[string]any{
		"initial_interval":          "2s",
		"backoff_coefficient":       2.0,
		"maximum_interval":          "1m",
		"maximum_attempts":          7,
		"non_retryable_error_types": []any{"NotFound", "Validation"},
	})
	startOptions.Set(optionWorkflowCronSchedule, "*/5 * * * *")
	startOptions.Set(optionWorkflowMemo, map[string]any{"request_id": "req-1"})
	startOptions.Set(optionWorkflowTypedSearchAttributes, typedSearchAttributes)
	startOptions.Set(optionWorkflowEnableEagerStart, true)
	startOptions.Set(optionWorkflowStartDelay, uint64(1500))
	startOptions.Set(optionWorkflowStaticSummary, "orders summary")
	startOptions.Set(optionWorkflowStaticDetails, "orders details")
	startOptions.Set(optionWorkflowVersioningOverride, map[string]any{
		"mode": "pinned",
		"version": map[string]any{
			"deployment_name": "orders",
			"build_id":        "2026.02.06",
		},
	})
	startOptions.Set(optionWorkflowPriority, map[string]any{
		"priority_key":    100,
		"fairness_key":    "tenant-a",
		"fairness_weight": 1.25,
	})

	start := &process.Start{Options: startOptions}
	opts := client.StartWorkflowOptions{TaskQueue: "default-task-queue"}

	state, err := applyTemporalStartWorkflowOptions(&opts, start)
	require.NoError(t, err)

	assert.True(t, state.hasConflictPolicy)
	assert.True(t, state.hasErrorOnStarted)

	assert.Equal(t, "orders:workflow-1", opts.ID)
	assert.Equal(t, "orders-queue", opts.TaskQueue)
	assert.Equal(t, 5*time.Minute, opts.WorkflowExecutionTimeout)
	assert.Equal(t, time.Minute, opts.WorkflowRunTimeout)
	assert.Equal(t, 5*time.Second, opts.WorkflowTaskTimeout)
	assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, opts.WorkflowIDConflictPolicy)
	assert.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY, opts.WorkflowIDReusePolicy)
	assert.False(t, opts.WorkflowExecutionErrorWhenAlreadyStarted)
	require.NotNil(t, opts.RetryPolicy)
	assert.Equal(t, 2*time.Second, opts.RetryPolicy.InitialInterval)
	assert.Equal(t, 2.0, opts.RetryPolicy.BackoffCoefficient)
	assert.Equal(t, time.Minute, opts.RetryPolicy.MaximumInterval)
	assert.Equal(t, int32(7), opts.RetryPolicy.MaximumAttempts)
	assert.ElementsMatch(t, []string{"NotFound", "Validation"}, opts.RetryPolicy.NonRetryableErrorTypes)
	assert.Equal(t, "*/5 * * * *", opts.CronSchedule)
	assert.Equal(t, map[string]any{"request_id": "req-1"}, opts.Memo)
	assert.Equal(t, typedSearchAttributes, opts.TypedSearchAttributes)
	assert.True(t, opts.EnableEagerStart)
	assert.Equal(t, 1500*time.Millisecond, opts.StartDelay)
	assert.Equal(t, "orders summary", opts.StaticSummary)
	assert.Equal(t, "orders details", opts.StaticDetails)
	assert.Equal(t, 100, opts.Priority.PriorityKey)
	assert.Equal(t, "tenant-a", opts.Priority.FairnessKey)
	assert.InDelta(t, 1.25, opts.Priority.FairnessWeight, 0.0001)

	override, ok := opts.VersioningOverride.(*client.PinnedVersioningOverride)
	require.True(t, ok)
	require.NotNil(t, override)
	assert.Equal(t, "orders", override.Version.DeploymentName)
	assert.Equal(t, "2026.02.06", override.Version.BuildID)
}

func TestApplyTemporalStartWorkflowOptions_KeepsDefaultTaskQueueWhenUnset(t *testing.T) {
	opts := client.StartWorkflowOptions{TaskQueue: "default-task-queue"}
	start := &process.Start{Options: attrs.NewBag()}

	_, err := applyTemporalStartWorkflowOptions(&opts, start)
	require.NoError(t, err)
	assert.Equal(t, "default-task-queue", opts.TaskQueue)
}

func TestApplyTemporalStartWorkflowOptions_MapTypedSearchAttributes(t *testing.T) {
	startOptions := attrs.NewBag()
	startOptions.Set(optionWorkflowTypedSearchAttributes, map[string]any{"CustomKeywordField": "tenant-a"})
	start := &process.Start{Options: startOptions}

	var opts client.StartWorkflowOptions
	_, err := applyTemporalStartWorkflowOptions(&opts, start)
	require.NoError(t, err)
	got, ok := opts.TypedSearchAttributes.GetKeyword(temporal.NewSearchAttributeKeyKeyword("CustomKeywordField"))
	require.True(t, ok)
	assert.Equal(t, "tenant-a", got)
}
