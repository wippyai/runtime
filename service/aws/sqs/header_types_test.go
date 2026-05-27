// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
)

// SQS MessageAttributes support three DataTypes: String, Number, Binary.
// Storing everything as "String" loses type information across a round-trip
// — a Lua handler that publishes { priority = 5 } gets back "5" (a string),
// which breaks arithmetic downstream. applyHeaders must pick the narrowest
// SQS DataType that preserves the Go type.

func TestApplyHeaders_IntUsesNumberDataType(t *testing.T) {
	entry := &types.SendMessageBatchRequestEntry{}
	bag := attrs.NewBag()
	bag.Set("priority", 5)

	applyHeaders(entry, bag)

	v, ok := entry.MessageAttributes["priority"]
	if assert.True(t, ok, "attribute must be emitted") {
		assert.Equal(t, "Number", aws.ToString(v.DataType))
		assert.Equal(t, "5", aws.ToString(v.StringValue))
	}
}

func TestApplyHeaders_FloatUsesNumberDataType(t *testing.T) {
	entry := &types.SendMessageBatchRequestEntry{}
	bag := attrs.NewBag()
	bag.Set("weight", 1.5)

	applyHeaders(entry, bag)

	v := entry.MessageAttributes["weight"]
	assert.Equal(t, "Number", aws.ToString(v.DataType))
	assert.Equal(t, "1.5", aws.ToString(v.StringValue))
}

func TestApplyHeaders_BytesUseBinaryDataType(t *testing.T) {
	entry := &types.SendMessageBatchRequestEntry{}
	bag := attrs.NewBag()
	bag.Set("blob", []byte{0x01, 0x02, 0x03})

	applyHeaders(entry, bag)

	v := entry.MessageAttributes["blob"]
	assert.Equal(t, "Binary", aws.ToString(v.DataType))
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, v.BinaryValue)
	assert.Nil(t, v.StringValue)
}

func TestApplyHeaders_BoolUsesCustomStringType(t *testing.T) {
	entry := &types.SendMessageBatchRequestEntry{}
	bag := attrs.NewBag()
	bag.Set("urgent", true)

	applyHeaders(entry, bag)

	v := entry.MessageAttributes["urgent"]
	// SQS has no bool DataType; use the "String.bool" custom suffix AWS
	// supports so the receive path can restore the Go bool.
	assert.Equal(t, "String.bool", aws.ToString(v.DataType))
	assert.Equal(t, "true", aws.ToString(v.StringValue))
}

func TestApplyHeaders_StringKeepsStringDataType(t *testing.T) {
	entry := &types.SendMessageBatchRequestEntry{}
	bag := attrs.NewBag()
	bag.Set("tenant", "acme")

	applyHeaders(entry, bag)

	v := entry.MessageAttributes["tenant"]
	assert.Equal(t, "String", aws.ToString(v.DataType))
	assert.Equal(t, "acme", aws.ToString(v.StringValue))
}

// Round-trip: applyHeaders then applyReceivedAttributes must recover the
// original Go type for int / float / bool / []byte / string.
func TestHeaderRoundTrip_PreservesTypes(t *testing.T) {
	entry := &types.SendMessageBatchRequestEntry{}
	src := attrs.NewBag()
	src.Set("priority", 5)
	src.Set("weight", 1.5)
	src.Set("urgent", true)
	src.Set("blob", []byte{0x01, 0x02})
	src.Set("tenant", "acme")

	applyHeaders(entry, src)

	// Simulate the consumer-side SQS message with the same attributes.
	received := types.Message{MessageAttributes: entry.MessageAttributes}
	out := applyReceivedAttributes(received)

	assert.Equal(t, int64(5), toInt64(out["priority"]), "priority must round-trip as a number")
	assert.InDelta(t, 1.5, toFloat64(out["weight"]), 0.0001, "weight must round-trip as a float")
	assert.Equal(t, true, out["urgent"], "urgent must round-trip as bool")
	assert.Equal(t, []byte{0x01, 0x02}, out["blob"], "blob must round-trip as bytes")
	assert.Equal(t, "acme", out["tenant"], "tenant stays a string")
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
