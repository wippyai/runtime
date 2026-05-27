// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	"go.uber.org/zap"
)

// GetQueueInfo on an unknown queue must return an apierror carrying the
// canonical NotFound kind, so callers can branch on kind rather than parse
// strings.
func TestGetQueueInfoUnknownQueueReturnsApierror(t *testing.T) {
	d := NewDriver(registry.ParseID("test:sqs"), &sqsapi.Config{}, aws.Config{}, nil, zap.NewNop())
	_, err := d.GetQueueInfo(context.Background(), registry.ParseID("missing:queue"))

	require.Error(t, err)
	var re apierror.Error
	require.ErrorAs(t, err, &re, "error must implement apierror.Error")
	assert.Equal(t, apierror.NotFound, re.Kind(), "kind should be NotFound for unknown queue")
}

// Publish on an unknown queue returns an apierror NotFound — the manager
// relies on this to short-circuit without a broker round-trip.
func TestPublishUnknownQueueReturnsApierror(t *testing.T) {
	d := NewDriver(registry.ParseID("test:sqs"), &sqsapi.Config{}, aws.Config{}, nil, zap.NewNop())
	msg := queueapi.AcquireMessage(nil)
	defer queueapi.ReleaseMessage(msg)

	err := d.Publish(context.Background(), registry.ParseID("missing:queue"), msg)
	require.Error(t, err)
	var re apierror.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, apierror.NotFound, re.Kind())
}
