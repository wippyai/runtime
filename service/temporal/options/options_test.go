package options

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	enumspb "go.temporal.io/api/enums/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/temporal"
)

func TestApplyActivityOptions_AllMappedFields(t *testing.T) {
	bag := attrs.NewBag()
	bag.Set(OptionActivityID, "activity-1")
	bag.Set(OptionActivityTaskQueue, "activities-queue")
	bag.Set(OptionActivityScheduleToClose, "2m")
	bag.Set(OptionActivityScheduleToStart, int64(1500))
	bag.Set(OptionActivityStartToClose, uint64(2500))
	bag.Set(OptionActivityHeartbeatTimeout, 5000.0)
	bag.Set(OptionActivityWaitForCancel, true)
	bag.Set(OptionActivityRetryPolicy, map[string]any{
		"initial_interval":          "1s",
		"backoff_coefficient":       2.0,
		"maximum_interval":          "20s",
		"maximum_attempts":          5,
		"non_retryable_error_types": []any{"NotFound"},
	})
	bag.Set(OptionActivityDisableEager, true)
	bag.Set(OptionActivityVersioningIntent, "compatible")
	bag.Set(OptionActivitySummary, "activity summary")
	bag.Set(OptionActivityPriority, map[string]any{
		"priority_key":    7,
		"fairness_key":    "tenant-a",
		"fairness_weight": 1.5,
	})

	opts := &bindings.ExecuteActivityOptions{
		TaskQueueName:       "default-queue",
		StartToCloseTimeout: 10 * time.Minute,
	}
	err := ApplyActivityOptions(opts, bag)
	require.NoError(t, err)

	assert.Equal(t, "activity-1", opts.ActivityID)
	assert.Equal(t, "activities-queue", opts.TaskQueueName)
	assert.Equal(t, 2*time.Minute, opts.ScheduleToCloseTimeout)
	assert.Equal(t, 1500*time.Millisecond, opts.ScheduleToStartTimeout)
	assert.Equal(t, 2500*time.Millisecond, opts.StartToCloseTimeout)
	assert.Equal(t, 5*time.Second, opts.HeartbeatTimeout)
	assert.True(t, opts.WaitForCancellation)
	require.NotNil(t, opts.RetryPolicy)
	assert.Equal(t, int32(5), opts.RetryPolicy.MaximumAttempts)
	assert.Equal(t, "NotFound", opts.RetryPolicy.NonRetryableErrorTypes[0])
	assert.True(t, opts.DisableEagerExecution)
	expectedActivityIntent, err := parseVersioningIntent("test.intent", "compatible")
	require.NoError(t, err)
	assert.Equal(t, expectedActivityIntent, opts.VersioningIntent)
	assert.Equal(t, "activity summary", opts.Summary)
	require.NotNil(t, opts.Priority)
	assert.Equal(t, int32(7), opts.Priority.PriorityKey)
	assert.Equal(t, "tenant-a", opts.Priority.FairnessKey)
	assert.InDelta(t, 1.5, opts.Priority.FairnessWeight, 0.0001)
}

func TestApplyChildWorkflowOptions_AllMappedFields(t *testing.T) {
	typedSearchAttributes := temporal.NewSearchAttributes(
		temporal.NewSearchAttributeKeyKeyword("CustomKeywordField").ValueSet("tenant-a"),
	)

	bag := attrs.NewBag()
	bag.Set(OptionWorkflowID, "child-workflow-id")
	bag.Set(OptionWorkflowTaskQueue, "child-queue")
	bag.Set(OptionWorkflowExecutionTimeout, "5m")
	bag.Set(OptionWorkflowRunTimeout, int64(60000))
	bag.Set(OptionWorkflowTaskTimeout, uint64(5000))
	bag.Set(OptionWorkflowIDConflictPolicy, "use_existing")
	bag.Set(OptionWorkflowIDReusePolicy, "allow_duplicate_failed_only")
	bag.Set(OptionWorkflowRetryPolicy, map[string]any{
		"initial_interval":          "1s",
		"backoff_coefficient":       2.0,
		"maximum_interval":          "30s",
		"maximum_attempts":          3,
		"non_retryable_error_types": []any{"Validation"},
	})
	bag.Set(OptionWorkflowCronSchedule, "*/5 * * * *")
	bag.Set(OptionWorkflowMemo, map[string]any{"req": "1"})
	bag.Set(OptionWorkflowSearchAttributes, map[string]any{"CustomKeywordField": "tenant-a"})
	bag.Set(OptionWorkflowTypedSearchAttributes, typedSearchAttributes)
	bag.Set(OptionWorkflowStaticSummary, "child summary")
	bag.Set(OptionWorkflowStaticDetails, "child details")
	bag.Set(OptionWorkflowPriority, map[string]any{
		"priority_key":    10,
		"fairness_key":    "tenant-a",
		"fairness_weight": 1.25,
	})
	bag.Set(OptionWorkflowNamespace, "default")
	bag.Set(OptionWorkflowWaitForCancellation, true)
	bag.Set(OptionWorkflowParentClosePolicy, "abandon")
	bag.Set(OptionWorkflowVersioningIntent, "default")

	params := &bindings.ExecuteWorkflowParams{
		WorkflowOptions: bindings.WorkflowOptions{
			TaskQueueName: "default-queue",
		},
	}
	err := ApplyChildWorkflowOptions(params, bag)
	require.NoError(t, err)

	assert.Equal(t, "child-workflow-id", params.WorkflowID)
	assert.Equal(t, "child-queue", params.TaskQueueName)
	assert.Equal(t, 5*time.Minute, params.WorkflowExecutionTimeout)
	assert.Equal(t, time.Minute, params.WorkflowRunTimeout)
	assert.Equal(t, 5*time.Second, params.WorkflowTaskTimeout)
	assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, params.WorkflowIDConflictPolicy)
	assert.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY, params.WorkflowIDReusePolicy)
	require.NotNil(t, params.RetryPolicy)
	assert.Equal(t, int32(3), params.RetryPolicy.MaximumAttempts)
	assert.Equal(t, "Validation", params.RetryPolicy.NonRetryableErrorTypes[0])
	assert.Equal(t, "*/5 * * * *", params.CronSchedule)
	assert.Equal(t, map[string]any{"req": "1"}, params.Memo)
	assert.Equal(t, map[string]any{"CustomKeywordField": "tenant-a"}, params.SearchAttributes)
	assert.Equal(t, typedSearchAttributes, params.TypedSearchAttributes)
	assert.Equal(t, "child summary", params.StaticSummary)
	assert.Equal(t, "child details", params.StaticDetails)
	require.NotNil(t, params.Priority)
	assert.Equal(t, int32(10), params.Priority.PriorityKey)
	assert.Equal(t, "tenant-a", params.Priority.FairnessKey)
	assert.InDelta(t, 1.25, params.Priority.FairnessWeight, 0.0001)
	assert.Equal(t, "default", params.Namespace)
	assert.True(t, params.WaitForCancellation)
	assert.Equal(t, enumspb.PARENT_CLOSE_POLICY_ABANDON, params.ParentClosePolicy)
	expectedWorkflowIntent, err := parseVersioningIntent("test.intent", "default")
	require.NoError(t, err)
	assert.Equal(t, expectedWorkflowIntent, params.VersioningIntent)
}
