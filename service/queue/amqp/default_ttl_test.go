// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"testing"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

// DefaultMessageTTL on the AMQP driver config is a per-message expiration
// default: any message published without an explicit amqp.expiration should
// pick it up. Today the driver parses the value at config time but nothing
// reads it at publish time — the default silently does nothing.
func TestApplyDefaultMessageTTL_SetsWhenAbsent(t *testing.T) {
	pub := amqp091.Publishing{}
	applyDefaultMessageTTL(&pub, 30*time.Second)

	assert.Equal(t, "30000", pub.Expiration,
		"driver default must populate Expiration when caller did not")
}

func TestApplyDefaultMessageTTL_DoesNotOverrideCallerValue(t *testing.T) {
	pub := amqp091.Publishing{Expiration: "5000"}
	applyDefaultMessageTTL(&pub, 30*time.Second)

	assert.Equal(t, "5000", pub.Expiration,
		"caller-supplied amqp.expiration must win over the driver default")
}

func TestApplyDefaultMessageTTL_NoopWhenDefaultZero(t *testing.T) {
	pub := amqp091.Publishing{}
	applyDefaultMessageTTL(&pub, 0)

	assert.Empty(t, pub.Expiration, "zero default means 'do not set'")
}
