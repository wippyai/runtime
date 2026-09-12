// SPDX-License-Identifier: MPL-2.0
package peer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

type observationRun struct {
	client.WorkflowRun
	err error
}

func (r observationRun) Get(context.Context, interface{}) error { return r.err }

type observationClient struct {
	client.Client
	err error
}

func (c observationClient) GetWorkflow(context.Context, string, string) client.WorkflowRun {
	return observationRun{err: c.err}
}

func TestWorkflowObservationFailureIsNotProcessDeath(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, context.Canceled, serviceerror.NewUnavailable("network down"), serviceerror.NewNotFound("history unavailable"), errors.New("result decode failed")} {
		t.Run(err.Error(), func(t *testing.T) {
			router := &mockRouter{}
			r := NewReceiver(context.Background(), "temporal", observationClient{err: err}, router, nil)
			defer r.Stop()
			caller := pid.PID{Node: "local", Host: "process", UniqID: "observer"}
			watcher := &workflowWatcher{workflowID: "workflow", runID: "run", taskQueue: "queue", watching: true, monitors: map[string]pid.PID{caller.String(): caller}, links: map[string]pid.PID{caller.String(): caller}}
			r.watchers[watcher.workflowID] = watcher
			r.watchWorkflow(r.ctx, watcher)
			require.Empty(t, router.packages, "backend observation error must not emit Exit or LinkDown as workflow completion")
			require.Same(t, watcher, r.watchers[watcher.workflowID], "uncertainty must preserve observation ownership")
			require.False(t, watcher.watching, "failed observation must allow explicit restart")
		})
	}
}

func TestWorkflowTerminalResultStillNotifies(t *testing.T) {
	for _, err := range []error{nil, &temporal.WorkflowExecutionError{}} {
		t.Run(map[bool]string{true: "success", false: "terminal-failure"}[err == nil], func(t *testing.T) {
			router := &mockRouter{}
			r := NewReceiver(context.Background(), "temporal", observationClient{err: err}, router, nil)
			defer r.Stop()
			caller := pid.PID{Node: "local", Host: "process", UniqID: "observer"}
			watcher := &workflowWatcher{workflowID: "workflow", runID: "run", taskQueue: "queue", watching: true, monitors: map[string]pid.PID{caller.String(): caller}}
			r.watchers[watcher.workflowID] = watcher
			r.watchWorkflow(r.ctx, watcher)
			require.Len(t, router.packages, 1)
			require.Empty(t, r.watchers)
		})
	}
}

func TestUnresolvedObservationCannotSuspendReplacement(t *testing.T) {
	r := NewReceiver(context.Background(), "temporal", observationClient{}, &mockRouter{}, nil)
	defer r.Stop()
	old := &workflowWatcher{workflowID: "workflow", runID: "old", watching: true}
	replacement := &workflowWatcher{workflowID: "workflow", runID: "new", watching: true}
	r.watchers[replacement.workflowID] = replacement
	r.suspendWorkflowObservation(old, context.DeadlineExceeded)
	require.True(t, replacement.watching)
	require.Same(t, replacement, r.watchers[replacement.workflowID])
}
