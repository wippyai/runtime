// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

type securityAudienceClientInterceptor struct {
	interceptor.ClientInterceptorBase
}

type securityAudienceClientOutboundInterceptor struct {
	interceptor.ClientOutboundInterceptorBase
}

func NewSecurityAudienceInterceptor() interceptor.ClientInterceptor {
	return &securityAudienceClientInterceptor{}
}

func (i *securityAudienceClientInterceptor) InterceptClient(next interceptor.ClientOutboundInterceptor) interceptor.ClientOutboundInterceptor {
	return &securityAudienceClientOutboundInterceptor{
		ClientOutboundInterceptorBase: interceptor.ClientOutboundInterceptorBase{Next: next},
	}
}

func (i *securityAudienceClientOutboundInterceptor) ExecuteWorkflow(ctx context.Context, input *interceptor.ClientExecuteWorkflowInput) (client.WorkflowRun, error) {
	if input != nil && input.Options != nil {
		ctx = WithSecurityAudience(ctx, input.Options.ID)
	}
	return i.Next.ExecuteWorkflow(ctx, input)
}

func (i *securityAudienceClientOutboundInterceptor) SignalWithStartWorkflow(ctx context.Context, input *interceptor.ClientSignalWithStartWorkflowInput) (client.WorkflowRun, error) {
	if input != nil && input.Options != nil {
		ctx = WithSecurityAudience(ctx, input.Options.ID)
	}
	return i.Next.SignalWithStartWorkflow(ctx, input)
}

func (i *securityAudienceClientOutboundInterceptor) ExecuteActivity(ctx context.Context, input *interceptor.ClientExecuteActivityInput) (client.ActivityHandle, error) {
	if input != nil && input.Options != nil {
		ctx = WithSecurityAudience(ctx, input.Options.ID)
	}
	return i.Next.ExecuteActivity(ctx, input)
}

func (i *securityAudienceClientOutboundInterceptor) SignalWorkflow(ctx context.Context, input *interceptor.ClientSignalWorkflowInput) error {
	if input != nil {
		ctx = WithSecurityAudience(ctx, input.WorkflowID)
	}
	return i.Next.SignalWorkflow(ctx, input)
}

func (i *securityAudienceClientOutboundInterceptor) QueryWorkflow(ctx context.Context, input *interceptor.ClientQueryWorkflowInput) (converter.EncodedValue, error) {
	if input != nil {
		ctx = WithSecurityAudience(ctx, input.WorkflowID)
	}
	return i.Next.QueryWorkflow(ctx, input)
}

func (i *securityAudienceClientOutboundInterceptor) UpdateWorkflow(ctx context.Context, input *interceptor.ClientUpdateWorkflowInput) (client.WorkflowUpdateHandle, error) {
	if input != nil {
		ctx = WithSecurityAudience(ctx, input.WorkflowID)
	}
	return i.Next.UpdateWorkflow(ctx, input)
}

type workflowSecurityAudienceKey struct{}

type securityAudienceWorkerInterceptor struct {
	interceptor.WorkerInterceptorBase
}

type securityAudienceWorkflowInboundInterceptor struct {
	interceptor.WorkflowInboundInterceptorBase
}

type securityAudienceWorkflowOutboundInterceptor struct {
	interceptor.WorkflowOutboundInterceptorBase
	childSequence    uint64
	activitySequence uint64
}

func NewSecurityAudienceWorkerInterceptor() interceptor.WorkerInterceptor {
	return &securityAudienceWorkerInterceptor{}
}

func (i *securityAudienceWorkerInterceptor) InterceptWorkflow(_ workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &securityAudienceWorkflowInboundInterceptor{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
	}
}

func (i *securityAudienceWorkflowInboundInterceptor) Init(outbound interceptor.WorkflowOutboundInterceptor) error {
	return i.Next.Init(&securityAudienceWorkflowOutboundInterceptor{
		WorkflowOutboundInterceptorBase: interceptor.WorkflowOutboundInterceptorBase{Next: outbound},
	})
}

func (i *securityAudienceWorkflowOutboundInterceptor) ExecuteChildWorkflow(ctx workflow.Context, childWorkflowType string, args ...interface{}) workflow.ChildWorkflowFuture {
	if getWorkflowSecurity(ctx) == nil {
		return i.Next.ExecuteChildWorkflow(ctx, childWorkflowType, args...)
	}
	options := workflow.GetChildWorkflowOptions(ctx)
	if options.WorkflowID == "" {
		i.childSequence++
		execution := workflow.GetInfo(ctx).WorkflowExecution
		options.WorkflowID = fmt.Sprintf("%s-%s-child-%d", execution.ID, execution.RunID, i.childSequence)
		ctx = workflow.WithChildOptions(ctx, options)
	}
	ctx = workflow.WithValue(ctx, workflowSecurityAudienceKey{}, options.WorkflowID)
	return i.Next.ExecuteChildWorkflow(ctx, childWorkflowType, args...)
}

func (i *securityAudienceWorkflowOutboundInterceptor) ExecuteActivity(ctx workflow.Context, activityType string, args ...interface{}) workflow.Future {
	if getWorkflowSecurity(ctx) == nil {
		return i.Next.ExecuteActivity(ctx, activityType, args...)
	}
	options := workflow.GetActivityOptions(ctx)
	if options.ActivityID == "" {
		i.activitySequence++
		execution := workflow.GetInfo(ctx).WorkflowExecution
		options.ActivityID = fmt.Sprintf("%s-%s-activity-%d", execution.ID, execution.RunID, i.activitySequence)
		ctx = workflow.WithActivityOptions(ctx, options)
	}
	ctx = workflow.WithValue(ctx, workflowSecurityAudienceKey{}, options.ActivityID)
	return i.Next.ExecuteActivity(ctx, activityType, args...)
}

func (i *securityAudienceWorkflowOutboundInterceptor) SignalExternalWorkflow(ctx workflow.Context, workflowID, runID, signalName string, arg interface{}) workflow.Future {
	if getWorkflowSecurity(ctx) != nil {
		ctx = workflow.WithValue(ctx, workflowSecurityAudienceKey{}, workflowID)
	}
	return i.Next.SignalExternalWorkflow(ctx, workflowID, runID, signalName, arg)
}

func workflowSecurityAudience(ctx workflow.Context) string {
	audience, _ := ctx.Value(workflowSecurityAudienceKey{}).(string)
	return audience
}
