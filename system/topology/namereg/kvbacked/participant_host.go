// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/relay"
)

// participantHost composes the existing sysreg host with snapshot endpoints.
// Routing transfers the original package once; mixed control/event batches are
// refused before invoking a handler, avoiding partial batch ownership.
type participantHost struct {
	registry  *Service
	authority *participantReceiver
	client    *participantClient
	cleanup   *cleanupQueue
}

func (h *participantHost) Send(pkg *relay.Package) error {
	return h.SendContext(context.Background(), pkg)
}
func (h *participantHost) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pkg == nil || h.registry == nil {
		return fmt.Errorf("invalid registry host package or service")
	}
	topic := ""
	for _, message := range pkg.Messages {
		if message == nil {
			return fmt.Errorf("nil registry message")
		}
		switch message.Topic {
		case participantSnapshotRequestTopic, participantSnapshotResponseTopic, participantSnapshotFailureTopic, participantSnapshotRedirectTopic:
			if len(pkg.Messages) != 1 {
				return fmt.Errorf("snapshot messages cannot share a registry batch")
			}
			topic = message.Topic
		}
	}
	switch topic {
	case participantSnapshotRequestTopic:
		if h.authority == nil {
			return errParticipantUnavailable
		}
		return h.authority.SendContext(ctx, pkg)
	case participantSnapshotResponseTopic, participantSnapshotFailureTopic, participantSnapshotRedirectTopic:
		if h.client == nil {
			return errParticipantUnavailable
		}
		return h.client.SendContext(ctx, pkg)
	default:
		if h.cleanup == nil {
			return h.registry.Send(pkg)
		}
		owner, valid := h.registry.exitPackageOwner(pkg)
		if valid {
			if err := h.cleanup.enqueue(ctx, owner); err != nil {
				return err
			}
		}
		// Retain only the validated owner identity, never the package or result.
		relay.ReleasePackage(pkg)
		return nil
	}
}
