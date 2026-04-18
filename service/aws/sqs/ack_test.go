// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"errors"
	"testing"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

type captureClient struct {
	deleteCtx context.Context
	visCtx    context.Context
	deleteErr error
	visErr    error
}

func (c *captureClient) DeleteMessage(ctx context.Context, _ *awssqs.DeleteMessageInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	c.deleteCtx = ctx
	return &awssqs.DeleteMessageOutput{}, c.deleteErr
}

func (c *captureClient) ChangeMessageVisibility(ctx context.Context, _ *awssqs.ChangeMessageVisibilityInput, _ ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error) {
	c.visCtx = ctx
	return &awssqs.ChangeMessageVisibilityOutput{}, c.visErr
}

type ctxMarker struct{}

// The Ack callback must forward the caller's context to the underlying SDK
// call, not swallow it with context.Background(). A consumer that cancels its
// context should propagate that cancellation to the in-flight DeleteMessage.
func TestBuildAckPropagatesCallerContext(t *testing.T) {
	c := &captureClient{}
	ack := buildAck(c, "https://queue", "receipt-1")

	caller := context.WithValue(context.Background(), ctxMarker{}, "signal")
	require := assert.New(t)
	require.NoError(ack(caller))
	require.NotNil(c.deleteCtx, "DeleteMessage must be called")
	require.Equal("signal", c.deleteCtx.Value(ctxMarker{}), "caller ctx must reach the SDK call")
}

func TestBuildAckReturnsSDKError(t *testing.T) {
	wantErr := errors.New("delete failed")
	c := &captureClient{deleteErr: wantErr}
	ack := buildAck(c, "u", "r")
	assert.ErrorIs(t, ack(context.Background()), wantErr)
}

// Nack must also forward caller context and return SDK errors.
func TestBuildNackPropagatesCallerContext(t *testing.T) {
	c := &captureClient{}
	nack := buildNack(c, "https://queue", "receipt-1")

	caller := context.WithValue(context.Background(), ctxMarker{}, "signal")
	require := assert.New(t)
	require.NoError(nack(caller))
	require.NotNil(c.visCtx, "ChangeMessageVisibility must be called")
	require.Equal("signal", c.visCtx.Value(ctxMarker{}), "caller ctx must reach the SDK call")
}

func TestBuildNackReturnsSDKError(t *testing.T) {
	wantErr := errors.New("cmv failed")
	c := &captureClient{visErr: wantErr}
	nack := buildNack(c, "u", "r")
	assert.ErrorIs(t, nack(context.Background()), wantErr)
}

// The rest of the SQS driver wraps SDK errors in apierror (see Publish,
// DeclareQueue, GetQueueInfo). The Ack/Nack callbacks must do the same so
// callers can uniformly classify + trigger retries by inspecting
// apierror.Kind / Retryable. A raw SDK error leaks through as Unknown and
// defeats structured retry logic at the consumer layer.
func TestBuildAckWrapsSDKErrorAsApierror(t *testing.T) {
	sdkErr := errors.New("throttled")
	c := &captureClient{deleteErr: sdkErr}
	ack := buildAck(c, "u", "r")

	err := ack(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, sdkErr), "caller must still be able to unwrap to the SDK error")

	var ae apierror.Error
	require.True(t, errors.As(err, &ae), "Ack must return apierror.Error, got %T: %v", err, err)
	require.Equal(t, apierror.Unavailable, ae.Kind())
	require.Equal(t, apierror.True, ae.Retryable())
}

func TestBuildNackWrapsSDKErrorAsApierror(t *testing.T) {
	sdkErr := errors.New("throttled")
	c := &captureClient{visErr: sdkErr}
	nack := buildNack(c, "u", "r")

	err := nack(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, sdkErr), "caller must still be able to unwrap to the SDK error")

	var ae apierror.Error
	require.True(t, errors.As(err, &ae), "Nack must return apierror.Error, got %T: %v", err, err)
	require.Equal(t, apierror.Unavailable, ae.Kind())
	require.Equal(t, apierror.True, ae.Retryable())
}
