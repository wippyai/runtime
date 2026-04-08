// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// sendToMembers sends a message to each member PID via the router.
// It creates a defensive copy of the payloads slice for each message
// to prevent data corruption if a downstream consumer mutates the slice.
func sendToMembers(router relay.Receiver, logger *zap.Logger, from pid.PID, topic string, payloads payload.Payloads, members []pid.PID) int {
	sent := 0
	for _, target := range members {
		msg := relay.AcquireMessage()
		msg.Topic = topic
		// Defensive copy: each message gets its own slice so downstream
		// consumers can't corrupt other messages by mutating the slice.
		copied := make(payload.Payloads, len(payloads))
		copy(copied, payloads)
		msg.Payloads = copied
		pkg := relay.NewMessagePackage(from, target, msg)
		if err := router.Send(pkg); err != nil {
			logger.Debug("broadcast send failed",
				zap.String("target", target.String()),
				zap.Error(err),
			)
			continue
		}
		sent++
	}
	return sent
}
