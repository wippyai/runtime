// Package options maps wippy attribute bags to Temporal SDK option structs
// for workflow start, activity execution, and child workflow configuration.
package options

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/temporal"
	sdkworker "go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/proto"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
)

const (
	OptionWorkflowID                               = "workflow.id"
	OptionWorkflowTaskQueue                        = "workflow.task_queue"
	OptionWorkflowExecutionTimeout                 = "workflow.execution_timeout"
	OptionWorkflowRunTimeout                       = "workflow.run_timeout"
	OptionWorkflowTaskTimeout                      = "workflow.task_timeout"
	OptionWorkflowIDConflictPolicy                 = "workflow.id_conflict_policy"
	OptionWorkflowIDReusePolicy                    = "workflow.id_reuse_policy"
	OptionWorkflowExecutionErrorWhenAlreadyStarted = "workflow.execution_error_when_already_started"
	OptionWorkflowRetryPolicy                      = "workflow.retry_policy"
	OptionWorkflowCronSchedule                     = "workflow.cron_schedule"
	OptionWorkflowMemo                             = "workflow.memo"
	OptionWorkflowSearchAttributes                 = "workflow.search_attributes"
	OptionWorkflowTypedSearchAttributes            = OptionWorkflowSearchAttributes
	OptionWorkflowEnableEagerStart                 = "workflow.enable_eager_start"
	OptionWorkflowStartDelay                       = "workflow.start_delay"
	OptionWorkflowStaticSummary                    = "workflow.summary"
	OptionWorkflowStaticDetails                    = "workflow.details"
	OptionWorkflowVersioningOverride               = "workflow.versioning_override"
	OptionWorkflowPriority                         = "workflow.priority"

	OptionWorkflowNamespace           = "workflow.namespace"
	OptionWorkflowWaitForCancellation = "workflow.wait_for_cancellation"
	OptionWorkflowParentClosePolicy   = "workflow.parent_close_policy"
	OptionWorkflowVersioningIntent    = "workflow.versioning_intent"

	OptionActivityID               = "activity.id"
	OptionActivityTaskQueue        = "activity.task_queue"
	OptionActivityScheduleToClose  = "activity.schedule_to_close_timeout"
	OptionActivityScheduleToStart  = "activity.schedule_to_start_timeout"
	OptionActivityStartToClose     = "activity.start_to_close_timeout"
	OptionActivityHeartbeatTimeout = "activity.heartbeat_timeout"
	OptionActivityWaitForCancel    = "activity.wait_for_cancellation"
	OptionActivityRetryPolicy      = "activity.retry_policy"
	OptionActivityDisableEager     = "activity.disable_eager_execution"
	OptionActivityVersioningIntent = "activity.versioning_intent"
	OptionActivitySummary          = "activity.summary"
	OptionActivityPriority         = "activity.priority"

	legacyWorkflowSearchAttributes      = "temporal.workflow.search_attributes"
	legacyWorkflowTypedSearchAttributes = "temporal.workflow.typed_search_attributes"
	legacyWorkflowStaticSummary         = "temporal.workflow.static_summary"
	legacyWorkflowStaticDetails         = "temporal.workflow.static_details"

	WorkflowVersioningOverrideModePinned      = "pinned"
	WorkflowVersioningOverrideModeAutoUpgrade = "auto_upgrade"
)

var legacyOptionAliases = map[string][]string{
	OptionWorkflowID:                               {"temporal.workflow.id"},
	OptionWorkflowTaskQueue:                        {"temporal.workflow.task_queue"},
	OptionWorkflowExecutionTimeout:                 {"temporal.workflow.execution_timeout"},
	OptionWorkflowRunTimeout:                       {"temporal.workflow.run_timeout"},
	OptionWorkflowTaskTimeout:                      {"temporal.workflow.task_timeout"},
	OptionWorkflowIDConflictPolicy:                 {"temporal.workflow.id_conflict_policy"},
	OptionWorkflowIDReusePolicy:                    {"temporal.workflow.id_reuse_policy"},
	OptionWorkflowExecutionErrorWhenAlreadyStarted: {"temporal.workflow.execution_error_when_already_started"},
	OptionWorkflowRetryPolicy:                      {"temporal.workflow.retry_policy"},
	OptionWorkflowCronSchedule:                     {"temporal.workflow.cron_schedule"},
	OptionWorkflowMemo:                             {"temporal.workflow.memo"},
	OptionWorkflowSearchAttributes: {
		legacyWorkflowSearchAttributes,
		legacyWorkflowTypedSearchAttributes,
	},
	OptionWorkflowEnableEagerStart:    {"temporal.workflow.enable_eager_start"},
	OptionWorkflowStartDelay:          {"temporal.workflow.start_delay"},
	OptionWorkflowStaticSummary:       {legacyWorkflowStaticSummary},
	OptionWorkflowStaticDetails:       {legacyWorkflowStaticDetails},
	OptionWorkflowVersioningOverride:  {"temporal.workflow.versioning_override"},
	OptionWorkflowPriority:            {"temporal.workflow.priority"},
	OptionWorkflowNamespace:           {"temporal.workflow.namespace"},
	OptionWorkflowWaitForCancellation: {"temporal.workflow.wait_for_cancellation"},
	OptionWorkflowParentClosePolicy:   {"temporal.workflow.parent_close_policy"},
	OptionWorkflowVersioningIntent:    {"temporal.workflow.versioning_intent"},

	OptionActivityID:               {"temporal.activity.id"},
	OptionActivityTaskQueue:        {"temporal.activity.task_queue"},
	OptionActivityScheduleToClose:  {"temporal.activity.schedule_to_close_timeout"},
	OptionActivityScheduleToStart:  {"temporal.activity.schedule_to_start_timeout"},
	OptionActivityStartToClose:     {"temporal.activity.start_to_close_timeout"},
	OptionActivityHeartbeatTimeout: {"temporal.activity.heartbeat_timeout"},
	OptionActivityWaitForCancel:    {"temporal.activity.wait_for_cancellation"},
	OptionActivityRetryPolicy:      {"temporal.activity.retry_policy"},
	OptionActivityDisableEager:     {"temporal.activity.disable_eager_execution"},
	OptionActivityVersioningIntent: {"temporal.activity.versioning_intent"},
	OptionActivitySummary:          {"temporal.activity.summary"},
	OptionActivityPriority:         {"temporal.activity.priority"},
}

// StartOptionState tracks which optional fields were explicitly set during option application.
type StartOptionState struct {
	HasConflictPolicy bool
	HasErrorOnStarted bool
}

// ApplyStartWorkflowOptions reads temporal workflow options from an attribute bag
// and applies them to StartWorkflowOptions. Returns state indicating which optional
// fields were explicitly set, allowing callers to apply defaults for unset fields.
func ApplyStartWorkflowOptions(opts *client.StartWorkflowOptions, options attrs.Attributes) (StartOptionState, error) {
	var state StartOptionState
	if opts == nil || options == nil {
		return state, nil
	}

	if raw, ok := optionGet(options, OptionWorkflowID); ok {
		v, err := parseString(OptionWorkflowID, raw)
		if err != nil {
			return state, err
		}
		if v == "" {
			return state, fmt.Errorf("%s cannot be empty", OptionWorkflowID)
		}
		opts.ID = v
	}

	if raw, ok := optionGet(options, OptionWorkflowTaskQueue); ok {
		v, err := parseString(OptionWorkflowTaskQueue, raw)
		if err != nil {
			return state, err
		}
		if v == "" {
			return state, fmt.Errorf("%s cannot be empty", OptionWorkflowTaskQueue)
		}
		opts.TaskQueue = v
	}

	if raw, ok := optionGet(options, OptionWorkflowExecutionTimeout); ok {
		v, err := parseDuration(OptionWorkflowExecutionTimeout, raw)
		if err != nil {
			return state, err
		}
		opts.WorkflowExecutionTimeout = v
	}

	if raw, ok := optionGet(options, OptionWorkflowRunTimeout); ok {
		v, err := parseDuration(OptionWorkflowRunTimeout, raw)
		if err != nil {
			return state, err
		}
		opts.WorkflowRunTimeout = v
	}

	if raw, ok := optionGet(options, OptionWorkflowTaskTimeout); ok {
		v, err := parseDuration(OptionWorkflowTaskTimeout, raw)
		if err != nil {
			return state, err
		}
		opts.WorkflowTaskTimeout = v
	}

	if raw, ok := optionGet(options, OptionWorkflowIDConflictPolicy); ok {
		v, err := parseConflictPolicy(OptionWorkflowIDConflictPolicy, raw)
		if err != nil {
			return state, err
		}
		opts.WorkflowIDConflictPolicy = v
		state.HasConflictPolicy = true
	}

	if raw, ok := optionGet(options, OptionWorkflowIDReusePolicy); ok {
		v, err := parseReusePolicy(OptionWorkflowIDReusePolicy, raw)
		if err != nil {
			return state, err
		}
		opts.WorkflowIDReusePolicy = v
	}

	if raw, ok := optionGet(options, OptionWorkflowExecutionErrorWhenAlreadyStarted); ok {
		v, err := parseBool(OptionWorkflowExecutionErrorWhenAlreadyStarted, raw)
		if err != nil {
			return state, err
		}
		opts.WorkflowExecutionErrorWhenAlreadyStarted = v
		state.HasErrorOnStarted = true
	}

	if raw, ok := optionGet(options, OptionWorkflowRetryPolicy); ok {
		v, err := parseRetryPolicyTemporal(OptionWorkflowRetryPolicy, raw)
		if err != nil {
			return state, err
		}
		opts.RetryPolicy = v
	}

	if raw, ok := optionGet(options, OptionWorkflowCronSchedule); ok {
		v, err := parseString(OptionWorkflowCronSchedule, raw)
		if err != nil {
			return state, err
		}
		opts.CronSchedule = v
	}

	if raw, ok := optionGet(options, OptionWorkflowMemo); ok {
		v, err := parseMap(OptionWorkflowMemo, raw)
		if err != nil {
			return state, err
		}
		opts.Memo = v
	}

	if raw, ok := optionGet(options, OptionWorkflowTypedSearchAttributes); ok {
		v, err := parseTypedSearchAttributes(OptionWorkflowTypedSearchAttributes, raw)
		if err != nil {
			return state, err
		}
		opts.TypedSearchAttributes = v
	}

	if raw, ok := optionGet(options, OptionWorkflowEnableEagerStart); ok {
		v, err := parseBool(OptionWorkflowEnableEagerStart, raw)
		if err != nil {
			return state, err
		}
		opts.EnableEagerStart = v
	}

	if raw, ok := optionGet(options, OptionWorkflowStartDelay); ok {
		v, err := parseDuration(OptionWorkflowStartDelay, raw)
		if err != nil {
			return state, err
		}
		opts.StartDelay = v
	}

	if raw, ok := optionGet(options, OptionWorkflowStaticSummary); ok {
		v, err := parseString(OptionWorkflowStaticSummary, raw)
		if err != nil {
			return state, err
		}
		opts.StaticSummary = v
	}

	if raw, ok := optionGet(options, OptionWorkflowStaticDetails); ok {
		v, err := parseString(OptionWorkflowStaticDetails, raw)
		if err != nil {
			return state, err
		}
		opts.StaticDetails = v
	}

	if raw, ok := optionGet(options, OptionWorkflowVersioningOverride); ok {
		v, err := parseVersioningOverride(OptionWorkflowVersioningOverride, raw)
		if err != nil {
			return state, err
		}
		opts.VersioningOverride = v
	}

	if raw, ok := optionGet(options, OptionWorkflowPriority); ok {
		v, err := parsePriorityTemporal(OptionWorkflowPriority, raw)
		if err != nil {
			return state, err
		}
		opts.Priority = v
	}

	return state, nil
}

// ApplyActivityOptions reads temporal activity options from an attribute bag
// and applies them to ExecuteActivityOptions (timeouts, retry policy, task queue, etc.).
func ApplyActivityOptions(opts *bindings.ExecuteActivityOptions, options attrs.Attributes) error {
	if opts == nil || options == nil {
		return nil
	}

	if raw, ok := optionGet(options, OptionActivityID); ok {
		v, err := parseString(OptionActivityID, raw)
		if err != nil {
			return err
		}
		opts.ActivityID = v
	}
	if raw, ok := optionGet(options, OptionActivityTaskQueue); ok {
		v, err := parseString(OptionActivityTaskQueue, raw)
		if err != nil {
			return err
		}
		if v == "" {
			return fmt.Errorf("%s cannot be empty", OptionActivityTaskQueue)
		}
		opts.TaskQueueName = v
	}
	if raw, ok := optionGet(options, OptionActivityScheduleToClose); ok {
		v, err := parseDuration(OptionActivityScheduleToClose, raw)
		if err != nil {
			return err
		}
		opts.ScheduleToCloseTimeout = v
	}
	if raw, ok := optionGet(options, OptionActivityScheduleToStart); ok {
		v, err := parseDuration(OptionActivityScheduleToStart, raw)
		if err != nil {
			return err
		}
		opts.ScheduleToStartTimeout = v
	}
	if raw, ok := optionGet(options, OptionActivityStartToClose); ok {
		v, err := parseDuration(OptionActivityStartToClose, raw)
		if err != nil {
			return err
		}
		opts.StartToCloseTimeout = v
	}
	if raw, ok := optionGet(options, OptionActivityHeartbeatTimeout); ok {
		v, err := parseDuration(OptionActivityHeartbeatTimeout, raw)
		if err != nil {
			return err
		}
		opts.HeartbeatTimeout = v
	}
	if raw, ok := optionGet(options, OptionActivityWaitForCancel); ok {
		v, err := parseBool(OptionActivityWaitForCancel, raw)
		if err != nil {
			return err
		}
		opts.WaitForCancellation = v
	}
	if raw, ok := optionGet(options, OptionActivityRetryPolicy); ok {
		v, err := parseRetryPolicyPB(OptionActivityRetryPolicy, raw)
		if err != nil {
			return err
		}
		opts.RetryPolicy = v
	}
	if raw, ok := optionGet(options, OptionActivityDisableEager); ok {
		v, err := parseBool(OptionActivityDisableEager, raw)
		if err != nil {
			return err
		}
		opts.DisableEagerExecution = v
	}
	if raw, ok := optionGet(options, OptionActivityVersioningIntent); ok {
		v, err := parseVersioningIntent(OptionActivityVersioningIntent, raw)
		if err != nil {
			return err
		}
		opts.VersioningIntent = v
	}
	if raw, ok := optionGet(options, OptionActivitySummary); ok {
		v, err := parseString(OptionActivitySummary, raw)
		if err != nil {
			return err
		}
		opts.Summary = v
	}
	if raw, ok := optionGet(options, OptionActivityPriority); ok {
		v, err := parsePriorityPB(OptionActivityPriority, raw)
		if err != nil {
			return err
		}
		opts.Priority = v
	}

	return nil
}

// ApplyChildWorkflowOptions reads temporal child workflow options from an attribute bag
// and applies them to ExecuteWorkflowParams (timeouts, parent close policy, search attributes, etc.).
func ApplyChildWorkflowOptions(params *bindings.ExecuteWorkflowParams, options attrs.Attributes) error {
	if params == nil || options == nil {
		return nil
	}

	if raw, ok := optionGet(options, OptionWorkflowID); ok {
		v, err := parseString(OptionWorkflowID, raw)
		if err != nil {
			return err
		}
		if v == "" {
			return fmt.Errorf("%s cannot be empty", OptionWorkflowID)
		}
		params.WorkflowID = v
	}

	if raw, ok := optionGet(options, OptionWorkflowTaskQueue); ok {
		v, err := parseString(OptionWorkflowTaskQueue, raw)
		if err != nil {
			return err
		}
		if v == "" {
			return fmt.Errorf("%s cannot be empty", OptionWorkflowTaskQueue)
		}
		params.TaskQueueName = v
	}

	if raw, ok := optionGet(options, OptionWorkflowExecutionTimeout); ok {
		v, err := parseDuration(OptionWorkflowExecutionTimeout, raw)
		if err != nil {
			return err
		}
		params.WorkflowExecutionTimeout = v
	}

	if raw, ok := optionGet(options, OptionWorkflowRunTimeout); ok {
		v, err := parseDuration(OptionWorkflowRunTimeout, raw)
		if err != nil {
			return err
		}
		params.WorkflowRunTimeout = v
	}

	if raw, ok := optionGet(options, OptionWorkflowTaskTimeout); ok {
		v, err := parseDuration(OptionWorkflowTaskTimeout, raw)
		if err != nil {
			return err
		}
		params.WorkflowTaskTimeout = v
	}

	if raw, ok := optionGet(options, OptionWorkflowIDConflictPolicy); ok {
		v, err := parseConflictPolicy(OptionWorkflowIDConflictPolicy, raw)
		if err != nil {
			return err
		}
		params.WorkflowIDConflictPolicy = v
	}

	if raw, ok := optionGet(options, OptionWorkflowIDReusePolicy); ok {
		v, err := parseReusePolicy(OptionWorkflowIDReusePolicy, raw)
		if err != nil {
			return err
		}
		params.WorkflowIDReusePolicy = v
	}

	if raw, ok := optionGet(options, OptionWorkflowRetryPolicy); ok {
		v, err := parseRetryPolicyPB(OptionWorkflowRetryPolicy, raw)
		if err != nil {
			return err
		}
		params.RetryPolicy = v
	}

	if raw, ok := optionGet(options, OptionWorkflowCronSchedule); ok {
		v, err := parseString(OptionWorkflowCronSchedule, raw)
		if err != nil {
			return err
		}
		params.CronSchedule = v
	}

	if raw, ok := optionGet(options, OptionWorkflowMemo); ok {
		v, err := parseMap(OptionWorkflowMemo, raw)
		if err != nil {
			return err
		}
		params.Memo = v
	}

	if raw, ok := optionGet(options, OptionWorkflowTypedSearchAttributes); ok {
		v, err := parseTypedSearchAttributes(OptionWorkflowTypedSearchAttributes, raw)
		if err != nil {
			return err
		}
		params.TypedSearchAttributes = v
	}

	if raw, ok := optionGet(options, OptionWorkflowStaticSummary); ok {
		v, err := parseString(OptionWorkflowStaticSummary, raw)
		if err != nil {
			return err
		}
		params.StaticSummary = v
	}

	if raw, ok := optionGet(options, OptionWorkflowStaticDetails); ok {
		v, err := parseString(OptionWorkflowStaticDetails, raw)
		if err != nil {
			return err
		}
		params.StaticDetails = v
	}

	if raw, ok := optionGet(options, OptionWorkflowPriority); ok {
		v, err := parsePriorityPB(OptionWorkflowPriority, raw)
		if err != nil {
			return err
		}
		params.Priority = v
	}

	if raw, ok := optionGet(options, OptionWorkflowNamespace); ok {
		v, err := parseString(OptionWorkflowNamespace, raw)
		if err != nil {
			return err
		}
		params.Namespace = v
	}

	if raw, ok := optionGet(options, OptionWorkflowWaitForCancellation); ok {
		v, err := parseBool(OptionWorkflowWaitForCancellation, raw)
		if err != nil {
			return err
		}
		params.WaitForCancellation = v
	}

	if raw, ok := optionGet(options, OptionWorkflowParentClosePolicy); ok {
		v, err := parseParentClosePolicy(OptionWorkflowParentClosePolicy, raw)
		if err != nil {
			return err
		}
		params.ParentClosePolicy = v
	}

	if raw, ok := optionGet(options, OptionWorkflowVersioningIntent); ok {
		v, err := parseVersioningIntent(OptionWorkflowVersioningIntent, raw)
		if err != nil {
			return err
		}
		params.VersioningIntent = v
	}

	return nil
}

func parseString(key string, value any) (string, error) {
	v, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	return v, nil
}

func parseBool(key string, value any) (bool, error) {
	v, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean, got %T", key, value)
	}
	return v, nil
}

func parseDuration(key string, value any) (time.Duration, error) {
	switch v := value.(type) {
	case time.Duration:
		return v, nil
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("%s must be a duration string (e.g. \"5s\"): %w", key, err)
		}
		return d, nil
	case int:
		return time.Duration(v) * time.Millisecond, nil
	case int8:
		return time.Duration(v) * time.Millisecond, nil
	case int16:
		return time.Duration(v) * time.Millisecond, nil
	case int32:
		return time.Duration(v) * time.Millisecond, nil
	case int64:
		return time.Duration(v) * time.Millisecond, nil
	case uint:
		return time.Duration(v) * time.Millisecond, nil
	case uint8:
		return time.Duration(v) * time.Millisecond, nil
	case uint16:
		return time.Duration(v) * time.Millisecond, nil
	case uint32:
		return time.Duration(v) * time.Millisecond, nil
	case uint64:
		return time.Duration(v) * time.Millisecond, nil
	case float32:
		return time.Duration(math.Round(float64(v) * float64(time.Millisecond))), nil
	case float64:
		return time.Duration(math.Round(v * float64(time.Millisecond))), nil
	default:
		return 0, fmt.Errorf("%s must be a duration string or milliseconds number, got %T", key, value)
	}
}

func parseConflictPolicy(key string, value any) (enumspb.WorkflowIdConflictPolicy, error) {
	switch v := value.(type) {
	case enumspb.WorkflowIdConflictPolicy:
		return v, nil
	case int:
		return enumspb.WorkflowIdConflictPolicy(v), nil
	case int32:
		return enumspb.WorkflowIdConflictPolicy(v), nil
	case int64:
		return enumspb.WorkflowIdConflictPolicy(v), nil
	case float64:
		return enumspb.WorkflowIdConflictPolicy(int32(v)), nil
	case string:
		normalized := normalizeEnum(v, "WORKFLOW_ID_CONFLICT_POLICY_")
		p, err := enumspb.WorkflowIdConflictPolicyFromString(normalized)
		if err != nil {
			return 0, fmt.Errorf("%s has invalid value %q", key, v)
		}
		return p, nil
	default:
		return 0, fmt.Errorf("%s must be enum string/int, got %T", key, value)
	}
}

func parseReusePolicy(key string, value any) (enumspb.WorkflowIdReusePolicy, error) {
	switch v := value.(type) {
	case enumspb.WorkflowIdReusePolicy:
		return v, nil
	case int:
		return enumspb.WorkflowIdReusePolicy(v), nil
	case int32:
		return enumspb.WorkflowIdReusePolicy(v), nil
	case int64:
		return enumspb.WorkflowIdReusePolicy(v), nil
	case float64:
		return enumspb.WorkflowIdReusePolicy(int32(v)), nil
	case string:
		normalized := normalizeEnum(v, "WORKFLOW_ID_REUSE_POLICY_")
		p, err := enumspb.WorkflowIdReusePolicyFromString(normalized)
		if err != nil {
			return 0, fmt.Errorf("%s has invalid value %q", key, v)
		}
		return p, nil
	default:
		return 0, fmt.Errorf("%s must be enum string/int, got %T", key, value)
	}
}

func parseParentClosePolicy(key string, value any) (enumspb.ParentClosePolicy, error) {
	switch v := value.(type) {
	case enumspb.ParentClosePolicy:
		return v, nil
	case int:
		return enumspb.ParentClosePolicy(v), nil
	case int32:
		return enumspb.ParentClosePolicy(v), nil
	case int64:
		return enumspb.ParentClosePolicy(v), nil
	case float64:
		return enumspb.ParentClosePolicy(int32(v)), nil
	case string:
		normalized := normalizeEnum(v, "PARENT_CLOSE_POLICY_")
		p, err := enumspb.ParentClosePolicyFromString(normalized)
		if err != nil {
			return 0, fmt.Errorf("%s has invalid value %q", key, v)
		}
		return p, nil
	default:
		return 0, fmt.Errorf("%s must be enum string/int, got %T", key, value)
	}
}

func parseRetryPolicyTemporal(key string, value any) (*temporal.RetryPolicy, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *temporal.RetryPolicy:
		if v == nil {
			return nil, nil
		}
		return cloneTemporalRetryPolicy(v), nil
	case temporal.RetryPolicy:
		return &v, nil
	case *commonpb.RetryPolicy:
		return retryPolicyToTemporal(v), nil
	case commonpb.RetryPolicy:
		return retryPolicyToTemporal(&v), nil
	}

	pb, err := parseRetryPolicyPB(key, value)
	if err != nil {
		return nil, err
	}
	return retryPolicyToTemporal(pb), nil
}

func parseRetryPolicyPB(key string, value any) (*commonpb.RetryPolicy, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *commonpb.RetryPolicy:
		if v == nil {
			return nil, nil
		}
		cloned, ok := proto.Clone(v).(*commonpb.RetryPolicy)
		if !ok {
			return nil, fmt.Errorf("%s could not clone retry policy", key)
		}
		return cloned, nil
	case commonpb.RetryPolicy:
		return &v, nil
	case *temporal.RetryPolicy:
		if v == nil {
			return nil, nil
		}
		return retryPolicyFromTemporal(*v), nil
	case temporal.RetryPolicy:
		return retryPolicyFromTemporal(v), nil
	}

	m, err := parseMap(key, value)
	if err != nil {
		return nil, err
	}

	policy := &commonpb.RetryPolicy{}

	if raw, ok := mapGet(m, "initial_interval", "initialInterval"); ok {
		d, err := parseDuration(key+".initial_interval", raw)
		if err != nil {
			return nil, err
		}
		if d > 0 {
			policy.InitialInterval = durationpb.New(d)
		}
	}
	if raw, ok := mapGet(m, "backoff_coefficient", "backoffCoefficient"); ok {
		f, err := parseFloat64(key+".backoff_coefficient", raw)
		if err != nil {
			return nil, err
		}
		policy.BackoffCoefficient = f
	}
	if raw, ok := mapGet(m, "maximum_interval", "maximumInterval"); ok {
		d, err := parseDuration(key+".maximum_interval", raw)
		if err != nil {
			return nil, err
		}
		if d > 0 {
			policy.MaximumInterval = durationpb.New(d)
		}
	}
	if raw, ok := mapGet(m, "maximum_attempts", "maximumAttempts"); ok {
		i, err := parseInt32(key+".maximum_attempts", raw)
		if err != nil {
			return nil, err
		}
		policy.MaximumAttempts = i
	}
	if raw, ok := mapGet(m, "non_retryable_error_types", "nonRetryableErrorTypes"); ok {
		values, err := parseStringSlice(key+".non_retryable_error_types", raw)
		if err != nil {
			return nil, err
		}
		policy.NonRetryableErrorTypes = values
	}

	return policy, nil
}

func parseMap(key string, value any) (map[string]any, error) {
	switch v := value.(type) {
	case map[string]any:
		return cloneAnyMap(v), nil
	case attrs.Bag:
		return cloneAnyMap(map[string]any(v)), nil
	default:
		return nil, fmt.Errorf("%s must be a table/map, got %T", key, value)
	}
}

func parseTypedSearchAttributes(key string, value any) (temporal.SearchAttributes, error) {
	switch v := value.(type) {
	case temporal.SearchAttributes:
		return v, nil
	case *temporal.SearchAttributes:
		if v == nil {
			return temporal.SearchAttributes{}, nil
		}
		return *v, nil
	default:
		return temporal.SearchAttributes{}, fmt.Errorf("%s must be temporal.SearchAttributes, got %T", key, value)
	}
}

func parsePriorityTemporal(key string, value any) (temporal.Priority, error) {
	switch v := value.(type) {
	case temporal.Priority:
		return v, nil
	case *temporal.Priority:
		if v == nil {
			return temporal.Priority{}, nil
		}
		return *v, nil
	case *commonpb.Priority:
		return priorityFromPB(v), nil
	case commonpb.Priority:
		return priorityFromPB(&v), nil
	}

	m, err := parseMap(key, value)
	if err != nil {
		return temporal.Priority{}, err
	}

	priority := temporal.Priority{}

	if raw, ok := mapGet(m, "priority_key", "priorityKey"); ok {
		i, err := parseInt(key+".priority_key", raw)
		if err != nil {
			return temporal.Priority{}, err
		}
		priority.PriorityKey = i
	}
	if raw, ok := mapGet(m, "fairness_key", "fairnessKey"); ok {
		s, err := parseString(key+".fairness_key", raw)
		if err != nil {
			return temporal.Priority{}, err
		}
		priority.FairnessKey = s
	}
	if raw, ok := mapGet(m, "fairness_weight", "fairnessWeight"); ok {
		f, err := parseFloat64(key+".fairness_weight", raw)
		if err != nil {
			return temporal.Priority{}, err
		}
		priority.FairnessWeight = float32(f)
	}

	return priority, nil
}

func parsePriorityPB(key string, value any) (*commonpb.Priority, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *commonpb.Priority:
		if v == nil {
			return nil, nil
		}
		cloned, ok := proto.Clone(v).(*commonpb.Priority)
		if !ok {
			return nil, fmt.Errorf("%s could not clone priority", key)
		}
		return cloned, nil
	case commonpb.Priority:
		return &v, nil
	case *temporal.Priority:
		if v == nil {
			return nil, nil
		}
		return priorityToPB(*v), nil
	case temporal.Priority:
		return priorityToPB(v), nil
	}

	m, err := parseMap(key, value)
	if err != nil {
		return nil, err
	}

	priority := &commonpb.Priority{}

	if raw, ok := mapGet(m, "priority_key", "priorityKey"); ok {
		i, err := parseInt32(key+".priority_key", raw)
		if err != nil {
			return nil, err
		}
		priority.PriorityKey = i
	}
	if raw, ok := mapGet(m, "fairness_key", "fairnessKey"); ok {
		s, err := parseString(key+".fairness_key", raw)
		if err != nil {
			return nil, err
		}
		priority.FairnessKey = s
	}
	if raw, ok := mapGet(m, "fairness_weight", "fairnessWeight"); ok {
		f, err := parseFloat64(key+".fairness_weight", raw)
		if err != nil {
			return nil, err
		}
		priority.FairnessWeight = float32(f)
	}

	return priority, nil
}

func parseVersioningOverride(key string, value any) (client.VersioningOverride, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *client.PinnedVersioningOverride:
		return v, nil
	case *client.AutoUpgradeVersioningOverride:
		return v, nil
	case client.VersioningOverride:
		return v, nil
	case string:
		mode := normalizeMode(v)
		if mode == WorkflowVersioningOverrideModeAutoUpgrade {
			return &client.AutoUpgradeVersioningOverride{}, nil
		}
		return nil, fmt.Errorf("%s mode %q requires map with version details", key, v)
	}

	m, err := parseMap(key, value)
	if err != nil {
		return nil, err
	}

	modeRaw, ok := mapGet(m, "mode")
	if !ok {
		return nil, fmt.Errorf("%s.mode is required", key)
	}
	mode, err := parseString(key+".mode", modeRaw)
	if err != nil {
		return nil, err
	}

	switch normalizeMode(mode) {
	case WorkflowVersioningOverrideModeAutoUpgrade:
		return &client.AutoUpgradeVersioningOverride{}, nil
	case WorkflowVersioningOverrideModePinned:
		versionMap := m
		if rawVersion, ok := mapGet(m, "version"); ok {
			versionMap, err = parseMap(key+".version", rawVersion)
			if err != nil {
				return nil, err
			}
		}

		deploymentRaw, ok := mapGet(versionMap, "deployment_name", "deploymentName")
		if !ok {
			return nil, fmt.Errorf("%s.version.deployment_name is required for pinned mode", key)
		}
		deployment, err := parseString(key+".version.deployment_name", deploymentRaw)
		if err != nil {
			return nil, err
		}
		buildRaw, ok := mapGet(versionMap, "build_id", "buildId")
		if !ok {
			return nil, fmt.Errorf("%s.version.build_id is required for pinned mode", key)
		}
		buildID, err := parseString(key+".version.build_id", buildRaw)
		if err != nil {
			return nil, err
		}

		return &client.PinnedVersioningOverride{
			Version: sdkworker.WorkerDeploymentVersion{
				DeploymentName: deployment,
				BuildID:        buildID,
			},
		}, nil
	default:
		return nil, fmt.Errorf("%s.mode must be %q or %q", key, WorkflowVersioningOverrideModePinned, WorkflowVersioningOverrideModeAutoUpgrade)
	}
}

//nolint:staticcheck // compatibility: maps legacy versioning intent option values.
func parseVersioningIntent(key string, value any) (temporal.VersioningIntent, error) {
	switch v := value.(type) {
	case temporal.VersioningIntent:
		return v, nil
	case int:
		return temporal.VersioningIntent(v), nil
	case int32:
		return temporal.VersioningIntent(v), nil
	case int64:
		return temporal.VersioningIntent(v), nil
	case float64:
		return temporal.VersioningIntent(int(v)), nil
	case string:
		switch normalizeMode(v) {
		case "unspecified":
			return temporal.VersioningIntentInheritBuildID, nil
		case "compatible":
			return temporal.VersioningIntentInheritBuildID, nil
		case "default":
			return temporal.VersioningIntentUseAssignmentRules, nil
		case "inherit_build_id", "inherit":
			return temporal.VersioningIntentInheritBuildID, nil
		case "use_assignment_rules", "assignment_rules":
			return temporal.VersioningIntentUseAssignmentRules, nil
		default:
			return temporal.VersioningIntentInheritBuildID, fmt.Errorf("%s has invalid value %q", key, v)
		}
	default:
		return temporal.VersioningIntentInheritBuildID, fmt.Errorf("%s must be enum string/int, got %T", key, value)
	}
}

func parseFloat64(key string, value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("%s must be a number, got %T", key, value)
	}
}

func parseInt32(key string, value any) (int32, error) {
	i, err := parseInt64(key, value)
	if err != nil {
		return 0, err
	}
	return int32(i), nil
}

func parseInt(key string, value any) (int, error) {
	i, err := parseInt64(key, value)
	if err != nil {
		return 0, err
	}
	return int(i), nil
}

func parseInt64(key string, value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("%s must be an integer number, got %v", key, value)
		}
		return int64(v), nil
	case float32:
		if math.Trunc(float64(v)) != float64(v) {
			return 0, fmt.Errorf("%s must be an integer number, got %v", key, value)
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer, got %T", key, value)
	}
}

func parseStringSlice(key string, value any) ([]string, error) {
	switch v := value.(type) {
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain only strings, got %T", key, item)
			}
			out = append(out, s)
		}
		return out, nil
	case string:
		return []string{v}, nil
	default:
		return nil, fmt.Errorf("%s must be string or string array, got %T", key, value)
	}
}

func optionGet(options attrs.Attributes, key string) (any, bool) {
	if options == nil {
		return nil, false
	}
	if v, ok := options.Get(key); ok {
		return v, true
	}
	aliases, ok := legacyOptionAliases[key]
	if !ok {
		return nil, false
	}
	for _, alias := range aliases {
		if v, ok := options.Get(alias); ok {
			return v, true
		}
	}
	return nil, false
}

func mapGet(m map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v, true
		}
	}
	for mk, mv := range m {
		for _, key := range keys {
			if strings.EqualFold(mk, key) {
				return mv, true
			}
		}
	}
	return nil, false
}

func normalizeEnum(value string, prefix string) string {
	s := strings.ToUpper(strings.TrimSpace(value))
	s = strings.ReplaceAll(s, "-", "_")
	if strings.HasPrefix(s, prefix) {
		return s
	}
	return prefix + s
}

func normalizeMode(value string) string {
	s := strings.ToLower(strings.TrimSpace(value))
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func retryPolicyFromTemporal(v temporal.RetryPolicy) *commonpb.RetryPolicy {
	policy := &commonpb.RetryPolicy{
		BackoffCoefficient:     v.BackoffCoefficient,
		MaximumAttempts:        v.MaximumAttempts,
		NonRetryableErrorTypes: append([]string(nil), v.NonRetryableErrorTypes...),
	}
	if v.InitialInterval > 0 {
		policy.InitialInterval = durationpb.New(v.InitialInterval)
	}
	if v.MaximumInterval > 0 {
		policy.MaximumInterval = durationpb.New(v.MaximumInterval)
	}
	return policy
}

func retryPolicyToTemporal(v *commonpb.RetryPolicy) *temporal.RetryPolicy {
	if v == nil {
		return nil
	}
	policy := &temporal.RetryPolicy{
		BackoffCoefficient:     v.GetBackoffCoefficient(),
		MaximumAttempts:        v.GetMaximumAttempts(),
		NonRetryableErrorTypes: append([]string(nil), v.GetNonRetryableErrorTypes()...),
	}
	if v.GetInitialInterval() != nil {
		policy.InitialInterval = v.GetInitialInterval().AsDuration()
	}
	if v.GetMaximumInterval() != nil {
		policy.MaximumInterval = v.GetMaximumInterval().AsDuration()
	}
	return policy
}

func cloneTemporalRetryPolicy(v *temporal.RetryPolicy) *temporal.RetryPolicy {
	if v == nil {
		return nil
	}
	cp := *v
	cp.NonRetryableErrorTypes = append([]string(nil), v.NonRetryableErrorTypes...)
	return &cp
}

func priorityToPB(v temporal.Priority) *commonpb.Priority {
	return &commonpb.Priority{
		PriorityKey:    int32(v.PriorityKey),
		FairnessKey:    v.FairnessKey,
		FairnessWeight: v.FairnessWeight,
	}
}

func priorityFromPB(v *commonpb.Priority) temporal.Priority {
	if v == nil {
		return temporal.Priority{}
	}
	return temporal.Priority{
		PriorityKey:    int(v.GetPriorityKey()),
		FairnessKey:    v.GetFairnessKey(),
		FairnessWeight: v.GetFairnessWeight(),
	}
}
