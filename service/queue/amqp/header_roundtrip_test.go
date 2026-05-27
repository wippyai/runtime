// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"testing"

	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
)

// A publisher that sets amqp.priority=0 still wants to receive it: zero is a
// valid priority value, not "unset". applyDeliveryHeaders previously skipped
// priority == 0, which made the bag inconsistent with what the publisher saw.
func TestApplyDeliveryHeaders_ZeroPriorityRoundTrips(t *testing.T) {
	dst := attrs.NewBag()
	applyDeliveryHeaders(dst, amqp091.Delivery{Priority: 0, DeliveryMode: amqp091.Persistent})

	v, ok := dst[publishPriority]
	if assert.True(t, ok, "zero priority must be present on the bag, not elided") {
		assert.Equal(t, 0, v)
	}
}

// DeliveryMode 0 (the wire default) is Transient in AMQP. The server always
// stamps one of {Transient, Persistent} on delivery — there is no true
// "unset". Always surface it so a handler that cares about durability can
// see the actual mode rather than receive a silently-dropped header.
func TestApplyDeliveryHeaders_DeliveryModeAlwaysPresent(t *testing.T) {
	dst := attrs.NewBag()
	applyDeliveryHeaders(dst, amqp091.Delivery{DeliveryMode: amqp091.Transient})

	v, ok := dst[publishDeliveryMod]
	if assert.True(t, ok) {
		assert.Equal(t, int(amqp091.Transient), v)
	}
}

// The "amqp." prefix has meaning only for typed fields (priority, expiration,
// etc.). For unknown keys buildPublishing must preserve the full key —
// otherwise "amqp.foo" and bare "foo" collapse to the same wire name and the
// publisher's namespace is silently erased.
func TestBuildPublishing_PreservesAmqpPrefixOnUnknownKeys(t *testing.T) {
	headers := attrs.NewBag()
	headers.Set("amqp.tenant", "acme")
	headers.Set("tenant_id", "42")

	pub := buildPublishing("id", []byte("x"), "application/json", headers)

	assert.Equal(t, "acme", pub.Headers["amqp.tenant"],
		"unknown amqp.* keys must pass through verbatim, not be stripped")
	assert.Equal(t, "42", pub.Headers["tenant_id"],
		"bare keys must also pass through verbatim")
}

// Round-trip: publish with "amqp.tenant" should yield "amqp.tenant" on the
// consumer side, and bare keys stay bare.
func TestHeaderRoundTrip_DistinguishesPrefixFromBare(t *testing.T) {
	src := attrs.NewBag()
	src.Set("amqp.tenant", "acme")
	src.Set("region", "us-east-1")

	pub := buildPublishing("id", []byte("x"), "application/json", src)

	dst := attrs.NewBag()
	applyDeliveryHeaders(dst, amqp091.Delivery{Headers: pub.Headers})

	assert.Equal(t, "acme", dst["amqp.tenant"])
	assert.Equal(t, "us-east-1", dst["region"])
}
