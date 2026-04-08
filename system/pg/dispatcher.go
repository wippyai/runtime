// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// Dispatcher handles pg command dispatching.
type Dispatcher struct {
	service *Service
	router  relay.Receiver
	logger  *zap.Logger
}

// NewDispatcher creates a new pg dispatcher.
func NewDispatcher(service *Service, router relay.Receiver, logger *zap.Logger) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Dispatcher{
		service: service,
		router:  router,
		logger:  logger,
	}
}

// RegisterAll registers all pg command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(pgapi.Join, dispatcher.HandlerFunc(d.handleJoin))
	register(pgapi.Leave, dispatcher.HandlerFunc(d.handleLeave))
	register(pgapi.GetMembers, dispatcher.HandlerFunc(d.handleGetMembers))
	register(pgapi.GetLocalMembers, dispatcher.HandlerFunc(d.handleGetLocalMembers))
	register(pgapi.WhichGroups, dispatcher.HandlerFunc(d.handleWhichGroups))
	register(pgapi.Broadcast, dispatcher.HandlerFunc(d.handleBroadcast))
	register(pgapi.BroadcastLocal, dispatcher.HandlerFunc(d.handleBroadcastLocal))
}

func (d *Dispatcher) handleJoin(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	joinCmd := cmd.(*pgapi.JoinCmd)

	err := d.service.Join(joinCmd.Group, joinCmd.Caller)
	receiver.CompleteYield(tag, pgapi.JoinResult{Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleLeave(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	leaveCmd := cmd.(*pgapi.LeaveCmd)

	err := d.service.Leave(leaveCmd.Group, leaveCmd.Caller)
	receiver.CompleteYield(tag, pgapi.LeaveResult{Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleGetMembers(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	getMembersCmd := cmd.(*pgapi.GetMembersCmd)

	members := d.service.GetMembers(getMembersCmd.Group)
	receiver.CompleteYield(tag, pgapi.GetMembersResult{Members: members}, nil)
	return nil
}

func (d *Dispatcher) handleGetLocalMembers(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	getLocalMembersCmd := cmd.(*pgapi.GetLocalMembersCmd)

	members := d.service.GetLocalMembers(getLocalMembersCmd.Group)
	receiver.CompleteYield(tag, pgapi.GetLocalMembersResult{Members: members}, nil)
	return nil
}

func (d *Dispatcher) handleWhichGroups(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	_ = cmd.(*pgapi.WhichGroupsCmd)

	groups := d.service.WhichGroups()
	receiver.CompleteYield(tag, pgapi.WhichGroupsResult{Groups: groups}, nil)
	return nil
}

func (d *Dispatcher) handleBroadcast(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	broadcastCmd := cmd.(*pgapi.BroadcastCmd)

	// Get members and send message to each
	members := d.service.GetMembers(broadcastCmd.Group)
	sent := sendToMembers(d.router, d.logger, broadcastCmd.From, broadcastCmd.Topic, broadcastCmd.Payloads, members)
	receiver.CompleteYield(tag, pgapi.BroadcastResult{Sent: sent}, nil)
	return nil
}

func (d *Dispatcher) handleBroadcastLocal(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	broadcastLocalCmd := cmd.(*pgapi.BroadcastLocalCmd)

	// Get local members and send message to each
	members := d.service.GetLocalMembers(broadcastLocalCmd.Group)
	sent := sendToMembers(d.router, d.logger, broadcastLocalCmd.From, broadcastLocalCmd.Topic, broadcastLocalCmd.Payloads, members)
	receiver.CompleteYield(tag, pgapi.BroadcastLocalResult{Sent: sent}, nil)
	return nil
}
