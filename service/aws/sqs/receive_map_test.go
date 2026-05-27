// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
)

// TestApplyReceivedAttributes_PreservesMessageAttributePrefix asserts that a
// publish → consume round-trip preserves MessageAttribute key names verbatim.
// A publish under "sqs.message_attributes.tenant" lands on the wire as a
// MessageAttribute of the same name and reappears under the same header key
// on consume (no silent prefix-strip).
func TestApplyReceivedAttributes_PreservesMessageAttributePrefix(t *testing.T) {
	sqsMsg := types.Message{
		MessageAttributes: map[string]types.MessageAttributeValue{
			"sqs.message_attributes.tenant":     {DataType: aws.String("String"), StringValue: aws.String("acme")},
			"sqs.message_attributes.request_id": {DataType: aws.String("String"), StringValue: aws.String("req-7")},
			"plain":                             {DataType: aws.String("String"), StringValue: aws.String("val")},
		},
	}

	headers := applyReceivedAttributes(sqsMsg)

	assert.Equal(t, "acme", headers.GetString("sqs.message_attributes.tenant", ""))
	assert.Equal(t, "req-7", headers.GetString("sqs.message_attributes.request_id", ""))
	assert.Equal(t, "val", headers.GetString("plain", ""))
}

// TestApplyReceivedAttributes_MessageGroupIDRoundTrip asserts that the SQS
// system attribute MessageGroupId comes back under the same key the publisher
// used.
func TestApplyReceivedAttributes_MessageGroupIDRoundTrip(t *testing.T) {
	sqsMsg := types.Message{
		Attributes: map[string]string{
			"MessageGroupId":         "group-42",
			"MessageDeduplicationId": "dedup-7",
		},
	}

	headers := applyReceivedAttributes(sqsMsg)

	assert.Equal(t, "group-42", headers.GetString(publishMessageGroup, ""))
	assert.Equal(t, "dedup-7", headers.GetString(publishDedupID, ""))
}

// TestApplyReceivedAttributes_EmptyMessage returns non-nil headers even when
// the SQS message has no attributes.
func TestApplyReceivedAttributes_EmptyMessage(t *testing.T) {
	headers := applyReceivedAttributes(types.Message{})
	assert.NotNil(t, headers)
	assert.Equal(t, 0, len(headers))
}
