package options

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/temporal"
	sdkworker "go.temporal.io/sdk/worker"
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

func TestApplyStartWorkflowOptions_LegacyAliasFallback(t *testing.T) {
	bag := attrs.NewBag()
	bag.Set("temporal.workflow.task_queue", "legacy-start-queue")
	bag.Set("temporal.workflow.static_summary", "legacy summary")

	opts := &client.StartWorkflowOptions{}
	_, err := ApplyStartWorkflowOptions(opts, bag)
	require.NoError(t, err)

	assert.Equal(t, "legacy-start-queue", opts.TaskQueue)
	assert.Equal(t, "legacy summary", opts.StaticSummary)
}

func TestApplyStartWorkflowOptions_CanonicalPrecedenceOverLegacy(t *testing.T) {
	bag := attrs.NewBag()
	bag.Set(OptionWorkflowTaskQueue, "canonical-start-queue")
	bag.Set("temporal.workflow.task_queue", "legacy-start-queue")

	opts := &client.StartWorkflowOptions{}
	_, err := ApplyStartWorkflowOptions(opts, bag)
	require.NoError(t, err)

	assert.Equal(t, "canonical-start-queue", opts.TaskQueue)
}

func TestApplyActivityOptions_CanonicalPrecedenceOverLegacy(t *testing.T) {
	bag := attrs.NewBag()
	bag.Set(OptionActivityTaskQueue, "canonical-activity-queue")
	bag.Set("temporal.activity.task_queue", "legacy-activity-queue")

	opts := &bindings.ExecuteActivityOptions{}
	err := ApplyActivityOptions(opts, bag)
	require.NoError(t, err)

	assert.Equal(t, "canonical-activity-queue", opts.TaskQueueName)
}

func TestApplyChildWorkflowOptions_LegacyTypedSearchAttributesAlias(t *testing.T) {
	typedSearchAttributes := temporal.NewSearchAttributes(
		temporal.NewSearchAttributeKeyKeyword("LegacyAliasField").ValueSet("alias-value"),
	)

	bag := attrs.NewBag()
	bag.Set("temporal.workflow.typed_search_attributes", typedSearchAttributes)

	params := &bindings.ExecuteWorkflowParams{
		WorkflowOptions: bindings.WorkflowOptions{TaskQueueName: "default-queue"},
	}
	err := ApplyChildWorkflowOptions(params, bag)
	require.NoError(t, err)

	assert.Equal(t, typedSearchAttributes, params.TypedSearchAttributes)
}

func TestApplyStartWorkflowOptions_AllMappedFields(t *testing.T) {
	bag := attrs.NewBag()
	bag.Set(OptionWorkflowID, "start-wf-id")
	bag.Set(OptionWorkflowTaskQueue, "start-queue")
	bag.Set(OptionWorkflowExecutionTimeout, "10m")
	bag.Set(OptionWorkflowRunTimeout, "5m")
	bag.Set(OptionWorkflowTaskTimeout, "30s")
	bag.Set(OptionWorkflowIDConflictPolicy, "fail")
	bag.Set(OptionWorkflowIDReusePolicy, "reject_duplicate")
	bag.Set(OptionWorkflowExecutionErrorWhenAlreadyStarted, true)
	bag.Set(OptionWorkflowRetryPolicy, map[string]any{
		"initial_interval":    "500ms",
		"backoff_coefficient": 1.5,
		"maximum_interval":    "1m",
		"maximum_attempts":    10,
	})
	bag.Set(OptionWorkflowCronSchedule, "0 * * * *")
	bag.Set(OptionWorkflowMemo, map[string]any{"env": "prod"})
	bag.Set(OptionWorkflowEnableEagerStart, true)
	bag.Set(OptionWorkflowStartDelay, "3s")
	bag.Set(OptionWorkflowStaticSummary, "my summary")
	bag.Set(OptionWorkflowStaticDetails, "my details")
	bag.Set(OptionWorkflowVersioningOverride, "auto_upgrade")
	bag.Set(OptionWorkflowPriority, map[string]any{
		"priority_key": 5,
		"fairness_key": "team-a",
	})

	opts := &client.StartWorkflowOptions{}
	state, err := ApplyStartWorkflowOptions(opts, bag)
	require.NoError(t, err)

	assert.Equal(t, "start-wf-id", opts.ID)
	assert.Equal(t, "start-queue", opts.TaskQueue)
	assert.Equal(t, 10*time.Minute, opts.WorkflowExecutionTimeout)
	assert.Equal(t, 5*time.Minute, opts.WorkflowRunTimeout)
	assert.Equal(t, 30*time.Second, opts.WorkflowTaskTimeout)
	assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL, opts.WorkflowIDConflictPolicy)
	assert.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE, opts.WorkflowIDReusePolicy)
	assert.True(t, opts.WorkflowExecutionErrorWhenAlreadyStarted)
	require.NotNil(t, opts.RetryPolicy)
	assert.Equal(t, 500*time.Millisecond, opts.RetryPolicy.InitialInterval)
	assert.Equal(t, 1.5, opts.RetryPolicy.BackoffCoefficient)
	assert.Equal(t, time.Minute, opts.RetryPolicy.MaximumInterval)
	assert.Equal(t, int32(10), opts.RetryPolicy.MaximumAttempts)
	assert.Equal(t, "0 * * * *", opts.CronSchedule)
	assert.Equal(t, map[string]interface{}{"env": "prod"}, opts.Memo)
	assert.True(t, opts.EnableEagerStart)
	assert.Equal(t, 3*time.Second, opts.StartDelay)
	assert.Equal(t, "my summary", opts.StaticSummary)
	assert.Equal(t, "my details", opts.StaticDetails)
	assert.IsType(t, &client.AutoUpgradeVersioningOverride{}, opts.VersioningOverride)
	assert.Equal(t, 5, opts.Priority.PriorityKey)
	assert.Equal(t, "team-a", opts.Priority.FairnessKey)

	assert.True(t, state.HasConflictPolicy)
	assert.True(t, state.HasErrorOnStarted)
}

func TestApplyStartWorkflowOptions_StateTracking(t *testing.T) {
	t.Run("state flags unset when options omitted", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowTaskQueue, "q")

		opts := &client.StartWorkflowOptions{}
		state, err := ApplyStartWorkflowOptions(opts, bag)
		require.NoError(t, err)
		assert.False(t, state.HasConflictPolicy)
		assert.False(t, state.HasErrorOnStarted)
	})

	t.Run("HasConflictPolicy set independently", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowIDConflictPolicy, "terminate_existing")

		opts := &client.StartWorkflowOptions{}
		state, err := ApplyStartWorkflowOptions(opts, bag)
		require.NoError(t, err)
		assert.True(t, state.HasConflictPolicy)
		assert.False(t, state.HasErrorOnStarted)
	})

	t.Run("HasErrorOnStarted set independently", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowExecutionErrorWhenAlreadyStarted, false)

		opts := &client.StartWorkflowOptions{}
		state, err := ApplyStartWorkflowOptions(opts, bag)
		require.NoError(t, err)
		assert.False(t, state.HasConflictPolicy)
		assert.True(t, state.HasErrorOnStarted)
	})
}

func TestApplyOptions_NilAndEmptyInputs(t *testing.T) {
	t.Run("start workflow nil opts", func(t *testing.T) {
		state, err := ApplyStartWorkflowOptions(nil, attrs.NewBag())
		require.NoError(t, err)
		assert.False(t, state.HasConflictPolicy)
	})

	t.Run("start workflow nil options bag", func(t *testing.T) {
		opts := &client.StartWorkflowOptions{}
		state, err := ApplyStartWorkflowOptions(opts, nil)
		require.NoError(t, err)
		assert.False(t, state.HasConflictPolicy)
	})

	t.Run("activity nil opts", func(t *testing.T) {
		err := ApplyActivityOptions(nil, attrs.NewBag())
		require.NoError(t, err)
	})

	t.Run("activity nil options bag", func(t *testing.T) {
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, nil)
		require.NoError(t, err)
	})

	t.Run("child workflow nil params", func(t *testing.T) {
		err := ApplyChildWorkflowOptions(nil, attrs.NewBag())
		require.NoError(t, err)
	})

	t.Run("child workflow nil options bag", func(t *testing.T) {
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, nil)
		require.NoError(t, err)
	})

	t.Run("empty bag leaves defaults", func(t *testing.T) {
		opts := &bindings.ExecuteActivityOptions{
			TaskQueueName:       "keep-this",
			StartToCloseTimeout: 5 * time.Minute,
		}
		err := ApplyActivityOptions(opts, attrs.NewBag())
		require.NoError(t, err)
		assert.Equal(t, "keep-this", opts.TaskQueueName)
		assert.Equal(t, 5*time.Minute, opts.StartToCloseTimeout)
	})
}

func TestApplyOptions_EmptyStringErrors(t *testing.T) {
	t.Run("empty workflow ID in start options", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowID, "")
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty task queue in start options", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowTaskQueue, "")
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty activity task queue", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityTaskQueue, "")
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty child workflow ID", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowID, "")
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty child workflow task queue", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowTaskQueue, "")
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})
}

func TestParseDuration(t *testing.T) {
	t.Run("string duration", func(t *testing.T) {
		d, err := parseDuration("test", "5s")
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, d)
	})

	t.Run("time.Duration passthrough", func(t *testing.T) {
		d, err := parseDuration("test", 3*time.Minute)
		require.NoError(t, err)
		assert.Equal(t, 3*time.Minute, d)
	})

	t.Run("int as milliseconds", func(t *testing.T) {
		d, err := parseDuration("test", 1500)
		require.NoError(t, err)
		assert.Equal(t, 1500*time.Millisecond, d)
	})

	t.Run("int64 as milliseconds", func(t *testing.T) {
		d, err := parseDuration("test", int64(2000))
		require.NoError(t, err)
		assert.Equal(t, 2*time.Second, d)
	})

	t.Run("float64 as milliseconds", func(t *testing.T) {
		d, err := parseDuration("test", float64(500))
		require.NoError(t, err)
		assert.Equal(t, 500*time.Millisecond, d)
	})

	t.Run("uint as milliseconds", func(t *testing.T) {
		d, err := parseDuration("test", uint(750))
		require.NoError(t, err)
		assert.Equal(t, 750*time.Millisecond, d)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := parseDuration("test", "not-a-duration")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duration string")
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parseDuration("test", struct{}{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duration string or milliseconds number")
	})
}

func TestParseBool(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		v, err := parseBool("test", true)
		require.NoError(t, err)
		assert.True(t, v)
	})

	t.Run("false", func(t *testing.T) {
		v, err := parseBool("test", false)
		require.NoError(t, err)
		assert.False(t, v)
	})

	t.Run("non-bool type", func(t *testing.T) {
		_, err := parseBool("test", "true")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a boolean")
	})
}

func TestParseString(t *testing.T) {
	t.Run("valid string", func(t *testing.T) {
		v, err := parseString("test", "hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", v)
	})

	t.Run("non-string type", func(t *testing.T) {
		_, err := parseString("test", 42)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})
}

func TestParseConflictPolicy(t *testing.T) {
	t.Run("string without prefix", func(t *testing.T) {
		v, err := parseConflictPolicy("test", "fail")
		require.NoError(t, err)
		assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL, v)
	})

	t.Run("string with prefix", func(t *testing.T) {
		v, err := parseConflictPolicy("test", "WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING")
		require.NoError(t, err)
		assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, v)
	})

	t.Run("int value", func(t *testing.T) {
		v, err := parseConflictPolicy("test", int(2))
		require.NoError(t, err)
		assert.Equal(t, enumspb.WorkflowIdConflictPolicy(2), v)
	})

	t.Run("float64 value", func(t *testing.T) {
		v, err := parseConflictPolicy("test", float64(3))
		require.NoError(t, err)
		assert.Equal(t, enumspb.WorkflowIdConflictPolicy(3), v)
	})

	t.Run("native enum type", func(t *testing.T) {
		v, err := parseConflictPolicy("test", enumspb.WORKFLOW_ID_CONFLICT_POLICY_TERMINATE_EXISTING)
		require.NoError(t, err)
		assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_TERMINATE_EXISTING, v)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := parseConflictPolicy("test", "nonexistent_policy")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value")
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseConflictPolicy("test", struct{}{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be enum string/int")
	})
}

func TestParseReusePolicy(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		v, err := parseReusePolicy("test", "allow_duplicate")
		require.NoError(t, err)
		assert.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE, v)
	})

	t.Run("float64 value", func(t *testing.T) {
		v, err := parseReusePolicy("test", float64(1))
		require.NoError(t, err)
		assert.Equal(t, enumspb.WorkflowIdReusePolicy(1), v)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := parseReusePolicy("test", "bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value")
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseReusePolicy("test", []int{1})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be enum string/int")
	})
}

func TestParseParentClosePolicy(t *testing.T) {
	t.Run("string terminate", func(t *testing.T) {
		v, err := parseParentClosePolicy("test", "terminate")
		require.NoError(t, err)
		assert.Equal(t, enumspb.PARENT_CLOSE_POLICY_TERMINATE, v)
	})

	t.Run("string abandon", func(t *testing.T) {
		v, err := parseParentClosePolicy("test", "abandon")
		require.NoError(t, err)
		assert.Equal(t, enumspb.PARENT_CLOSE_POLICY_ABANDON, v)
	})

	t.Run("string request_cancel", func(t *testing.T) {
		v, err := parseParentClosePolicy("test", "request_cancel")
		require.NoError(t, err)
		assert.Equal(t, enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL, v)
	})

	t.Run("native enum type", func(t *testing.T) {
		v, err := parseParentClosePolicy("test", enumspb.PARENT_CLOSE_POLICY_TERMINATE)
		require.NoError(t, err)
		assert.Equal(t, enumspb.PARENT_CLOSE_POLICY_TERMINATE, v)
	})

	t.Run("int value", func(t *testing.T) {
		v, err := parseParentClosePolicy("test", int(1))
		require.NoError(t, err)
		assert.Equal(t, enumspb.ParentClosePolicy(1), v)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := parseParentClosePolicy("test", "invalid_policy")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value")
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseParentClosePolicy("test", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be enum string/int")
	})
}

//nolint:staticcheck // coverage for legacy build-id intent compatibility behavior.
func TestParseVersioningIntent(t *testing.T) {
	t.Run("unspecified", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "unspecified")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentInheritBuildID, v)
	})

	t.Run("compatible", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "compatible")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentInheritBuildID, v)
	})

	t.Run("default", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "default")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentUseAssignmentRules, v)
	})

	t.Run("inherit_build_id", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "inherit_build_id")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentInheritBuildID, v)
	})

	t.Run("use_assignment_rules", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "use_assignment_rules")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentUseAssignmentRules, v)
	})

	t.Run("inherit alias", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "inherit")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentInheritBuildID, v)
	})

	t.Run("assignment_rules alias", func(t *testing.T) {
		v, err := parseVersioningIntent("test", "assignment_rules")
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentUseAssignmentRules, v)
	})

	t.Run("int value", func(t *testing.T) {
		v, err := parseVersioningIntent("test", int(0))
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntent(0), v)
	})

	t.Run("float64 value", func(t *testing.T) {
		v, err := parseVersioningIntent("test", float64(1))
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntent(1), v)
	})

	t.Run("native type", func(t *testing.T) {
		v, err := parseVersioningIntent("test", temporal.VersioningIntentUseAssignmentRules)
		require.NoError(t, err)
		assert.Equal(t, temporal.VersioningIntentUseAssignmentRules, v)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := parseVersioningIntent("test", "bogus_intent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid value")
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseVersioningIntent("test", struct{}{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be enum string/int")
	})
}

func TestParseVersioningOverride(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		v, err := parseVersioningOverride("test", nil)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("string auto_upgrade", func(t *testing.T) {
		v, err := parseVersioningOverride("test", "auto_upgrade")
		require.NoError(t, err)
		assert.IsType(t, &client.AutoUpgradeVersioningOverride{}, v)
	})

	t.Run("string pinned requires map", func(t *testing.T) {
		_, err := parseVersioningOverride("test", "pinned")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires map with version details")
	})

	t.Run("map auto_upgrade", func(t *testing.T) {
		v, err := parseVersioningOverride("test", map[string]any{"mode": "auto_upgrade"})
		require.NoError(t, err)
		assert.IsType(t, &client.AutoUpgradeVersioningOverride{}, v)
	})

	t.Run("map pinned with deployment and build", func(t *testing.T) {
		v, err := parseVersioningOverride("test", map[string]any{
			"mode":            "pinned",
			"deployment_name": "my-deploy",
			"build_id":        "v1.0",
		})
		require.NoError(t, err)
		pinned, ok := v.(*client.PinnedVersioningOverride)
		require.True(t, ok)
		assert.Equal(t, "my-deploy", pinned.Version.DeploymentName)
		assert.Equal(t, "v1.0", pinned.Version.BuildID)
	})

	t.Run("map pinned with nested version", func(t *testing.T) {
		v, err := parseVersioningOverride("test", map[string]any{
			"mode": "pinned",
			"version": map[string]any{
				"deployment_name": "nested-deploy",
				"build_id":        "v2.0",
			},
		})
		require.NoError(t, err)
		pinned, ok := v.(*client.PinnedVersioningOverride)
		require.True(t, ok)
		assert.Equal(t, "nested-deploy", pinned.Version.DeploymentName)
		assert.Equal(t, "v2.0", pinned.Version.BuildID)
	})

	t.Run("map pinned missing deployment_name", func(t *testing.T) {
		_, err := parseVersioningOverride("test", map[string]any{
			"mode":     "pinned",
			"build_id": "v1.0",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment_name is required")
	})

	t.Run("map pinned missing build_id", func(t *testing.T) {
		_, err := parseVersioningOverride("test", map[string]any{
			"mode":            "pinned",
			"deployment_name": "my-deploy",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build_id is required")
	})

	t.Run("map missing mode", func(t *testing.T) {
		_, err := parseVersioningOverride("test", map[string]any{
			"deployment_name": "my-deploy",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mode is required")
	})

	t.Run("map invalid mode", func(t *testing.T) {
		_, err := parseVersioningOverride("test", map[string]any{
			"mode": "invalid_mode",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be")
	})

	t.Run("native PinnedVersioningOverride", func(t *testing.T) {
		input := &client.PinnedVersioningOverride{
			Version: sdkworker.WorkerDeploymentVersion{
				DeploymentName: "native-deploy",
				BuildID:        "native-build",
			},
		}
		v, err := parseVersioningOverride("test", input)
		require.NoError(t, err)
		assert.Equal(t, input, v)
	})

	t.Run("native AutoUpgradeVersioningOverride", func(t *testing.T) {
		input := &client.AutoUpgradeVersioningOverride{}
		v, err := parseVersioningOverride("test", input)
		require.NoError(t, err)
		assert.Equal(t, input, v)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parseVersioningOverride("test", 42)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a table/map")
	})
}

func TestParseRetryPolicyTemporal(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		v, err := parseRetryPolicyTemporal("test", nil)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("temporal.RetryPolicy value", func(t *testing.T) {
		input := temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumAttempts: 5,
		}
		v, err := parseRetryPolicyTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, time.Second, v.InitialInterval)
		assert.Equal(t, int32(5), v.MaximumAttempts)
	})

	t.Run("*temporal.RetryPolicy pointer", func(t *testing.T) {
		input := &temporal.RetryPolicy{
			InitialInterval:        2 * time.Second,
			BackoffCoefficient:     2.0,
			NonRetryableErrorTypes: []string{"Err1"},
		}
		v, err := parseRetryPolicyTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, 2*time.Second, v.InitialInterval)
		assert.Equal(t, 2.0, v.BackoffCoefficient)
		assert.Equal(t, []string{"Err1"}, v.NonRetryableErrorTypes)
		// Verify it is a clone
		input.NonRetryableErrorTypes[0] = "Modified"
		assert.Equal(t, "Err1", v.NonRetryableErrorTypes[0])
	})

	t.Run("nil *temporal.RetryPolicy", func(t *testing.T) {
		var input *temporal.RetryPolicy
		v, err := parseRetryPolicyTemporal("test", input)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("map with all fields", func(t *testing.T) {
		input := map[string]any{
			"initial_interval":          "1s",
			"backoff_coefficient":       2.0,
			"maximum_interval":          "30s",
			"maximum_attempts":          float64(3),
			"non_retryable_error_types": []any{"NotFound", "Validation"},
		}
		v, err := parseRetryPolicyTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, time.Second, v.InitialInterval)
		assert.Equal(t, 2.0, v.BackoffCoefficient)
		assert.Equal(t, 30*time.Second, v.MaximumInterval)
		assert.Equal(t, int32(3), v.MaximumAttempts)
		assert.Equal(t, []string{"NotFound", "Validation"}, v.NonRetryableErrorTypes)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parseRetryPolicyTemporal("test", "not a policy")
		require.Error(t, err)
	})
}

func TestParseRetryPolicyPB(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		v, err := parseRetryPolicyPB("test", nil)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("nil *commonpb.RetryPolicy", func(t *testing.T) {
		var input *commonpb.RetryPolicy
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("commonpb.RetryPolicy value", func(t *testing.T) {
		input := &commonpb.RetryPolicy{
			MaximumAttempts: 7,
		}
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, int32(7), v.MaximumAttempts)
	})

	t.Run("temporal.RetryPolicy value converts to PB", func(t *testing.T) {
		input := temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 1.5,
			MaximumAttempts:    4,
		}
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, int32(4), v.MaximumAttempts)
		assert.Equal(t, 1.5, v.BackoffCoefficient)
	})

	t.Run("nil *temporal.RetryPolicy", func(t *testing.T) {
		var input *temporal.RetryPolicy
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("map with camelCase keys", func(t *testing.T) {
		input := map[string]any{
			"initialInterval":    "2s",
			"backoffCoefficient": 3.0,
			"maximumAttempts":    float64(5),
		}
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, int32(5), v.MaximumAttempts)
		assert.Equal(t, 3.0, v.BackoffCoefficient)
	})

	t.Run("map with string slice for error types", func(t *testing.T) {
		input := map[string]any{
			"non_retryable_error_types": []string{"Err1"},
		}
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, []string{"Err1"}, v.NonRetryableErrorTypes)
	})

	t.Run("map with single string error type", func(t *testing.T) {
		input := map[string]any{
			"non_retryable_error_types": "SingleError",
		}
		v, err := parseRetryPolicyPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, []string{"SingleError"}, v.NonRetryableErrorTypes)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parseRetryPolicyPB("test", 42)
		require.Error(t, err)
	})

	t.Run("invalid duration in map", func(t *testing.T) {
		input := map[string]any{
			"initial_interval": "bad-dur",
		}
		_, err := parseRetryPolicyPB("test", input)
		require.Error(t, err)
	})

	t.Run("non-string in error types slice", func(t *testing.T) {
		input := map[string]any{
			"non_retryable_error_types": []any{42},
		}
		_, err := parseRetryPolicyPB("test", input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must contain only strings")
	})
}

func TestParseMap(t *testing.T) {
	t.Run("map[string]any", func(t *testing.T) {
		input := map[string]any{"key": "val"}
		v, err := parseMap("test", input)
		require.NoError(t, err)
		assert.Equal(t, "val", v["key"])
		// Verify clone
		input["key"] = "modified"
		assert.Equal(t, "val", v["key"])
	})

	t.Run("attrs.Bag", func(t *testing.T) {
		bag := attrs.Bag{"key": "val"}
		v, err := parseMap("test", bag)
		require.NoError(t, err)
		assert.Equal(t, "val", v["key"])
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parseMap("test", "not-a-map")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a table/map")
	})
}

func TestParseTypedSearchAttributes(t *testing.T) {
	t.Run("value type", func(t *testing.T) {
		input := temporal.NewSearchAttributes(
			temporal.NewSearchAttributeKeyKeyword("Field").ValueSet("val"),
		)
		v, err := parseTypedSearchAttributes("test", input)
		require.NoError(t, err)
		assert.Equal(t, input, v)
	})

	t.Run("pointer type", func(t *testing.T) {
		input := temporal.NewSearchAttributes(
			temporal.NewSearchAttributeKeyKeyword("Field").ValueSet("val"),
		)
		v, err := parseTypedSearchAttributes("test", &input)
		require.NoError(t, err)
		assert.Equal(t, input, v)
	})

	t.Run("nil pointer", func(t *testing.T) {
		var input *temporal.SearchAttributes
		v, err := parseTypedSearchAttributes("test", input)
		require.NoError(t, err)
		assert.Equal(t, temporal.SearchAttributes{}, v)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parseTypedSearchAttributes("test", "not-sa")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be temporal.SearchAttributes or map")
	})

	t.Run("map[string]any input", func(t *testing.T) {
		v, err := parseTypedSearchAttributes("test", map[string]any{
			"Tenant": "tenant-a",
			"Score":  float64(7),
			"Tags":   []any{"a", "b"},
		})
		require.NoError(t, err)

		tenant, ok := v.GetKeyword(temporal.NewSearchAttributeKeyKeyword("Tenant"))
		require.True(t, ok)
		assert.Equal(t, "tenant-a", tenant)

		score, ok := v.GetInt64(temporal.NewSearchAttributeKeyInt64("Score"))
		require.True(t, ok)
		assert.EqualValues(t, 7, score)

		tags, ok := v.GetKeywordList(temporal.NewSearchAttributeKeyKeywordList("Tags"))
		require.True(t, ok)
		assert.Equal(t, []string{"a", "b"}, tags)
	})

	t.Run("attrs.Bag input", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set("Tenant", "tenant-a")
		v, err := parseTypedSearchAttributes("test", bag)
		require.NoError(t, err)
		tenant, ok := v.GetKeyword(temporal.NewSearchAttributeKeyKeyword("Tenant"))
		require.True(t, ok)
		assert.Equal(t, "tenant-a", tenant)
	})

	t.Run("keyword list invalid item type", func(t *testing.T) {
		_, err := parseTypedSearchAttributes("test", map[string]any{
			"Tags": []any{"a", 2},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyword list item")
	})
}

func TestParsePriorityTemporal(t *testing.T) {
	t.Run("temporal.Priority value", func(t *testing.T) {
		input := temporal.Priority{PriorityKey: 3, FairnessKey: "t"}
		v, err := parsePriorityTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, input, v)
	})

	t.Run("*temporal.Priority pointer", func(t *testing.T) {
		input := &temporal.Priority{PriorityKey: 4}
		v, err := parsePriorityTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, *input, v)
	})

	t.Run("nil *temporal.Priority", func(t *testing.T) {
		var input *temporal.Priority
		v, err := parsePriorityTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, temporal.Priority{}, v)
	})

	t.Run("*commonpb.Priority converts", func(t *testing.T) {
		input := &commonpb.Priority{PriorityKey: 8, FairnessKey: "pk"}
		v, err := parsePriorityTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, 8, v.PriorityKey)
		assert.Equal(t, "pk", v.FairnessKey)
	})

	t.Run("map with camelCase keys", func(t *testing.T) {
		input := map[string]any{
			"priorityKey":    5,
			"fairnessKey":    "team",
			"fairnessWeight": 2.0,
		}
		v, err := parsePriorityTemporal("test", input)
		require.NoError(t, err)
		assert.Equal(t, 5, v.PriorityKey)
		assert.Equal(t, "team", v.FairnessKey)
		assert.InDelta(t, 2.0, float64(v.FairnessWeight), 0.001)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parsePriorityTemporal("test", "not-priority")
		require.Error(t, err)
	})
}

func TestParsePriorityPB(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		v, err := parsePriorityPB("test", nil)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("nil *commonpb.Priority", func(t *testing.T) {
		var input *commonpb.Priority
		v, err := parsePriorityPB("test", input)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("commonpb.Priority value", func(t *testing.T) {
		input := &commonpb.Priority{PriorityKey: 9}
		v, err := parsePriorityPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, int32(9), v.PriorityKey)
	})

	t.Run("temporal.Priority converts to PB", func(t *testing.T) {
		input := temporal.Priority{PriorityKey: 6, FairnessKey: "k"}
		v, err := parsePriorityPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, int32(6), v.PriorityKey)
		assert.Equal(t, "k", v.FairnessKey)
	})

	t.Run("nil *temporal.Priority", func(t *testing.T) {
		var input *temporal.Priority
		v, err := parsePriorityPB("test", input)
		require.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("map", func(t *testing.T) {
		input := map[string]any{"priority_key": 2}
		v, err := parsePriorityPB("test", input)
		require.NoError(t, err)
		assert.Equal(t, int32(2), v.PriorityKey)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := parsePriorityPB("test", "bad")
		require.Error(t, err)
	})
}

func TestParseInt64(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		v, err := parseInt64("test", 42)
		require.NoError(t, err)
		assert.Equal(t, int64(42), v)
	})

	t.Run("float64 integer value", func(t *testing.T) {
		v, err := parseInt64("test", float64(10))
		require.NoError(t, err)
		assert.Equal(t, int64(10), v)
	})

	t.Run("float64 fractional value", func(t *testing.T) {
		_, err := parseInt64("test", float64(10.5))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be an integer number")
	})

	t.Run("float32 fractional value", func(t *testing.T) {
		_, err := parseInt64("test", float32(3.14))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be an integer number")
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseInt64("test", "10")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be an integer")
	})
}

func TestParseFloat64(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		v, err := parseFloat64("test", float64(3.14))
		require.NoError(t, err)
		assert.InDelta(t, 3.14, v, 0.001)
	})

	t.Run("int", func(t *testing.T) {
		v, err := parseFloat64("test", 5)
		require.NoError(t, err)
		assert.Equal(t, float64(5), v)
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseFloat64("test", "3.14")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a number")
	})
}

func TestParseStringSlice(t *testing.T) {
	t.Run("[]string", func(t *testing.T) {
		v, err := parseStringSlice("test", []string{"a", "b"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, v)
	})

	t.Run("[]any with strings", func(t *testing.T) {
		v, err := parseStringSlice("test", []any{"x", "y"})
		require.NoError(t, err)
		assert.Equal(t, []string{"x", "y"}, v)
	})

	t.Run("[]any with non-string", func(t *testing.T) {
		_, err := parseStringSlice("test", []any{"ok", 42})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must contain only strings")
	})

	t.Run("single string", func(t *testing.T) {
		v, err := parseStringSlice("test", "single")
		require.NoError(t, err)
		assert.Equal(t, []string{"single"}, v)
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseStringSlice("test", 42)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be string or string array")
	})
}

func TestMapGet_CaseInsensitive(t *testing.T) {
	m := map[string]any{
		"InitialInterval": "5s",
	}

	v, ok := mapGet(m, "initial_interval", "initialInterval")
	assert.True(t, ok)
	assert.Equal(t, "5s", v)
}

func TestNormalizeEnum(t *testing.T) {
	t.Run("adds prefix", func(t *testing.T) {
		result := normalizeEnum("fail", "WORKFLOW_ID_CONFLICT_POLICY_")
		assert.Equal(t, "WORKFLOW_ID_CONFLICT_POLICY_FAIL", result)
	})

	t.Run("keeps existing prefix", func(t *testing.T) {
		result := normalizeEnum("WORKFLOW_ID_CONFLICT_POLICY_FAIL", "WORKFLOW_ID_CONFLICT_POLICY_")
		assert.Equal(t, "WORKFLOW_ID_CONFLICT_POLICY_FAIL", result)
	})

	t.Run("normalizes dashes to underscores", func(t *testing.T) {
		result := normalizeEnum("use-existing", "WORKFLOW_ID_CONFLICT_POLICY_")
		assert.Equal(t, "WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING", result)
	})

	t.Run("trims whitespace", func(t *testing.T) {
		result := normalizeEnum("  fail  ", "WORKFLOW_ID_CONFLICT_POLICY_")
		assert.Equal(t, "WORKFLOW_ID_CONFLICT_POLICY_FAIL", result)
	})
}

func TestApplyActivityOptions_TypeErrors(t *testing.T) {
	t.Run("non-string activity ID", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityID, 123)
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("non-bool wait_for_cancellation", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityWaitForCancel, "true")
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a boolean")
	})

	t.Run("invalid duration format", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityScheduleToClose, "not-valid")
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duration string")
	})

	t.Run("invalid retry policy type", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityRetryPolicy, "bad")
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("invalid versioning intent", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityVersioningIntent, struct{}{})
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("invalid priority type", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionActivityPriority, "bad")
		opts := &bindings.ExecuteActivityOptions{}
		err := ApplyActivityOptions(opts, bag)
		require.Error(t, err)
	})
}

func TestApplyChildWorkflowOptions_TypeErrors(t *testing.T) {
	t.Run("non-string workflow ID", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowID, 123)
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("invalid duration", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowExecutionTimeout, struct{}{})
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
	})

	t.Run("invalid conflict policy string", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowIDConflictPolicy, "bad_policy")
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
	})

	t.Run("invalid parent close policy", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowParentClosePolicy, "invalid")
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
	})

	t.Run("invalid typed search attributes", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowTypedSearchAttributes, "not-sa")
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
	})

	t.Run("non-bool wait_for_cancellation", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowWaitForCancellation, "true")
		params := &bindings.ExecuteWorkflowParams{}
		err := ApplyChildWorkflowOptions(params, bag)
		require.Error(t, err)
	})
}

func TestApplyStartWorkflowOptions_TypeErrors(t *testing.T) {
	t.Run("non-string workflow ID", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowID, 123)
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("invalid duration", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowExecutionTimeout, []int{1})
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("non-bool enable_eager_start", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowEnableEagerStart, "yes")
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("non-bool execution_error_when_already_started", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowExecutionErrorWhenAlreadyStarted, 1)
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("invalid memo type", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowMemo, "not-a-map")
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
	})

	t.Run("invalid versioning override", func(t *testing.T) {
		bag := attrs.NewBag()
		bag.Set(OptionWorkflowVersioningOverride, 42)
		opts := &client.StartWorkflowOptions{}
		_, err := ApplyStartWorkflowOptions(opts, bag)
		require.Error(t, err)
	})
}
