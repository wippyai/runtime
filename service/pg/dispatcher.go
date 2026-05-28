// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/dispatcher"
	pgapi "github.com/wippyai/runtime/api/service/pg"
)

var errNoInstance = errors.New("pg: command has no instance set")

// Dispatcher handles pg command dispatching. It is fully stateless — each
// command carries a ScopeService reference so the dispatcher routes to the
// correct PG scope instance. Broadcast delivery is delegated to the Service.
type Dispatcher struct{}

// NewDispatcher creates a new pg dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// RegisterAll registers all pg command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(pgapi.Join, dispatcher.HandlerFunc(d.handleJoin))
	register(pgapi.Leave, dispatcher.HandlerFunc(d.handleLeave))
	register(pgapi.GetMembers, dispatcher.HandlerFunc(d.handleGetMembers))
	register(pgapi.GetLocalMembers, dispatcher.HandlerFunc(d.handleGetLocalMembers))
	register(pgapi.WhichGroups, dispatcher.HandlerFunc(d.handleWhichGroups))
	register(pgapi.WhichLocalGroups, dispatcher.HandlerFunc(d.handleWhichLocalGroups))
	register(pgapi.Broadcast, dispatcher.HandlerFunc(d.handleBroadcast))
	register(pgapi.BroadcastLocal, dispatcher.HandlerFunc(d.handleBroadcastLocal))
	register(pgapi.Monitor, dispatcher.HandlerFunc(d.handleMonitor))
	register(pgapi.Events, dispatcher.HandlerFunc(d.handleEvents))
	register(pgapi.JoinGroups, dispatcher.HandlerFunc(d.handleJoinGroups))
	register(pgapi.LeaveGroups, dispatcher.HandlerFunc(d.handleLeaveGroups))
}

func (d *Dispatcher) handleJoin(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	joinCmd := cmd.(*pgapi.JoinCmd)
	if joinCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.JoinResult{Error: errNoInstance}, nil)
		return nil
	}

	err := joinCmd.Instance.Join(joinCmd.Group, joinCmd.Caller)
	receiver.CompleteYield(tag, pgapi.JoinResult{Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleLeave(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	leaveCmd := cmd.(*pgapi.LeaveCmd)
	if leaveCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.LeaveResult{Error: errNoInstance}, nil)
		return nil
	}

	err := leaveCmd.Instance.Leave(leaveCmd.Group, leaveCmd.Caller)
	receiver.CompleteYield(tag, pgapi.LeaveResult{Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleGetMembers(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	getMembersCmd := cmd.(*pgapi.GetMembersCmd)
	if getMembersCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.GetMembersResult{}, nil)
		return nil
	}

	members := getMembersCmd.Instance.GetMembers(getMembersCmd.Group)
	receiver.CompleteYield(tag, pgapi.GetMembersResult{Members: members}, nil)
	return nil
}

func (d *Dispatcher) handleGetLocalMembers(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	getLocalMembersCmd := cmd.(*pgapi.GetLocalMembersCmd)
	if getLocalMembersCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.GetLocalMembersResult{}, nil)
		return nil
	}

	members := getLocalMembersCmd.Instance.GetLocalMembers(getLocalMembersCmd.Group)
	receiver.CompleteYield(tag, pgapi.GetLocalMembersResult{Members: members}, nil)
	return nil
}

func (d *Dispatcher) handleWhichGroups(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	whichGroupsCmd := cmd.(*pgapi.WhichGroupsCmd)
	if whichGroupsCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.WhichGroupsResult{}, nil)
		return nil
	}

	groups := whichGroupsCmd.Instance.WhichGroups()
	receiver.CompleteYield(tag, pgapi.WhichGroupsResult{Groups: groups}, nil)
	return nil
}

func (d *Dispatcher) handleWhichLocalGroups(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	whichLocalGroupsCmd := cmd.(*pgapi.WhichLocalGroupsCmd)
	if whichLocalGroupsCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.WhichLocalGroupsResult{}, nil)
		return nil
	}

	groups := whichLocalGroupsCmd.Instance.WhichLocalGroups()
	receiver.CompleteYield(tag, pgapi.WhichLocalGroupsResult{Groups: groups}, nil)
	return nil
}

func (d *Dispatcher) handleBroadcast(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	broadcastCmd := cmd.(*pgapi.BroadcastCmd)
	if broadcastCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.BroadcastResult{Error: errNoInstance}, nil)
		return nil
	}

	sent, err := broadcastCmd.Instance.Broadcast(broadcastCmd.From, broadcastCmd.Group, broadcastCmd.Topic, broadcastCmd.Payloads)
	receiver.CompleteYield(tag, pgapi.BroadcastResult{Sent: sent, Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleBroadcastLocal(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	broadcastLocalCmd := cmd.(*pgapi.BroadcastLocalCmd)
	if broadcastLocalCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.BroadcastLocalResult{Error: errNoInstance}, nil)
		return nil
	}

	sent, err := broadcastLocalCmd.Instance.BroadcastLocal(broadcastLocalCmd.From, broadcastLocalCmd.Group, broadcastLocalCmd.Topic, broadcastLocalCmd.Payloads)
	receiver.CompleteYield(tag, pgapi.BroadcastLocalResult{Sent: sent, Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleMonitor(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	monitorCmd := cmd.(*pgapi.MonitorCmd)
	if monitorCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.MonitorResult{}, nil)
		return nil
	}

	result := monitorCmd.Instance.Monitor(monitorCmd.Group, monitorCmd.PID, monitorCmd.Topic)
	receiver.CompleteYield(tag, result, nil)
	return nil
}

func (d *Dispatcher) handleEvents(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	eventsCmd := cmd.(*pgapi.EventsCmd)
	if eventsCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.EventsResult{}, nil)
		return nil
	}

	result := eventsCmd.Instance.Events(eventsCmd.PID, eventsCmd.Topic)
	receiver.CompleteYield(tag, result, nil)
	return nil
}
func (d *Dispatcher) handleJoinGroups(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	joinCmd := cmd.(*pgapi.JoinGroupsCmd)
	if joinCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.JoinGroupsResult{Error: errNoInstance}, nil)
		return nil
	}

	err := joinCmd.Instance.JoinGroups(joinCmd.Groups, joinCmd.Caller)
	receiver.CompleteYield(tag, pgapi.JoinGroupsResult{Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleLeaveGroups(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	leaveCmd := cmd.(*pgapi.LeaveGroupsCmd)
	if leaveCmd.Instance == nil {
		receiver.CompleteYield(tag, pgapi.LeaveGroupsResult{Error: errNoInstance}, nil)
		return nil
	}

	err := leaveCmd.Instance.LeaveGroups(leaveCmd.Groups, leaveCmd.Caller)
	receiver.CompleteYield(tag, pgapi.LeaveGroupsResult{Error: err}, nil)
	return nil
}
