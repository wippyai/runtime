// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	publishv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/publish/v1"
)

type PublishStatus int

const (
	PublishStatusUnspecified PublishStatus = iota
	PublishStatusPendingUpload
	PublishStatusProcessing
	PublishStatusValidating
	PublishStatusCompleted
	PublishStatusFailed
	PublishStatusCancelled
)

type PublishParams struct {
	Org          string
	Module       string
	Version      string
	Label        string
	ReleaseNotes string
	Digest       string
	Size         int64
	Protected    bool
}

type PublishResult struct {
	ExpiresAt time.Time
	PublishID string
	UploadURL string
}

type StatusResult struct {
	VersionID    string
	ErrorMessage string
	ErrorCode    string
	Status       PublishStatus
}

func (s *StatusResult) IsCompleted() bool {
	return s.Status == PublishStatusCompleted
}

func (s *StatusResult) IsFailed() bool {
	return s.Status == PublishStatusFailed
}

func (s *StatusResult) StatusString() string {
	switch s.Status {
	case PublishStatusPendingUpload:
		return "pending upload"
	case PublishStatusProcessing:
		return "processing"
	case PublishStatusValidating:
		return "validating"
	case PublishStatusCompleted:
		return "completed"
	case PublishStatusFailed:
		return "failed"
	case PublishStatusCancelled:
		return "canceled"
	default:
		return "unknown"
	}
}

func (c *Client) InitiatePublish(ctx context.Context, params *PublishParams) (*PublishResult, error) {
	req := &publishv1.InitiatePublishRequest{
		Module: &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_Name{
				Name: &modulev1.ModuleName{
					Org:  params.Org,
					Name: params.Module,
				},
			},
		},
		ExpectedSizeBytes: uint64(params.Size),
		Digest:            params.Digest,
		Protected:         params.Protected,
		ReleaseNotes:      params.ReleaseNotes,
	}

	if params.Version != "" {
		req.Target = &publishv1.InitiatePublishRequest_Version{
			Version: params.Version,
		}
	} else if params.Label != "" {
		req.Target = &publishv1.InitiatePublishRequest_Label{
			Label: params.Label,
		}
	} else {
		return nil, fmt.Errorf("either version or label must be specified")
	}

	resp, err := c.Publish.InitiatePublish(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	var expiresAt time.Time
	if resp.Msg.UploadExpiresAt != nil {
		expiresAt = resp.Msg.UploadExpiresAt.AsTime()
	}

	return &PublishResult{
		PublishID: resp.Msg.PublishId,
		UploadURL: resp.Msg.UploadUrl,
		ExpiresAt: expiresAt,
	}, nil
}

func (c *Client) ConfirmPublish(ctx context.Context, publishID string) error {
	req := &publishv1.ConfirmPublishRequest{
		PublishId: publishID,
	}

	_, err := c.Publish.ConfirmPublish(ctx, connect.NewRequest(req))
	return MapConnectError(err)
}

func (c *Client) GetPublishStatus(ctx context.Context, publishID string) (*StatusResult, error) {
	req := &publishv1.GetPublishStatusRequest{
		PublishId: publishID,
	}

	resp, err := c.Publish.GetPublishStatus(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	result := &StatusResult{
		Status: mapPublishStatus(resp.Msg.Status),
	}

	if resp.Msg.Details != nil {
		result.ErrorMessage = resp.Msg.Details.ErrorMessage
		result.ErrorCode = resp.Msg.Details.ErrorCode
		if resp.Msg.Details.Version != nil {
			result.VersionID = resp.Msg.Details.Version.Id
		}
	}

	return result, nil
}

func (c *Client) CancelPublish(ctx context.Context, publishID string) error {
	req := &publishv1.CancelPublishRequest{
		PublishId: publishID,
	}

	_, err := c.Publish.CancelPublish(ctx, connect.NewRequest(req))
	return MapConnectError(err)
}

func (c *Client) WaitForCompletion(ctx context.Context, publishID string, callback func(status *StatusResult)) (*StatusResult, error) {
	const pollInterval = 2 * time.Second

	checkStatus := func() (*StatusResult, bool, error) {
		status, err := c.GetPublishStatus(ctx, publishID)
		if err != nil {
			return nil, false, err
		}

		if callback != nil {
			callback(status)
		}

		switch status.Status {
		case PublishStatusCompleted:
			return status, true, nil
		case PublishStatusFailed:
			return status, true, fmt.Errorf("publish failed: %s", status.ErrorMessage)
		case PublishStatusCancelled:
			return status, true, fmt.Errorf("publish canceled")
		default:
			return status, false, nil
		}
	}

	// Check immediately
	if status, done, err := checkStatus(); done || err != nil {
		return status, err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for publish to complete: %w", ctx.Err())
		case <-ticker.C:
			if status, done, err := checkStatus(); done || err != nil {
				return status, err
			}
		}
	}
}

func mapPublishStatus(s publishv1.PublishStatus) PublishStatus {
	switch s {
	case publishv1.PublishStatus_PUBLISH_STATUS_PENDING_UPLOAD:
		return PublishStatusPendingUpload
	case publishv1.PublishStatus_PUBLISH_STATUS_PROCESSING:
		return PublishStatusProcessing
	case publishv1.PublishStatus_PUBLISH_STATUS_VALIDATING:
		return PublishStatusValidating
	case publishv1.PublishStatus_PUBLISH_STATUS_COMPLETED:
		return PublishStatusCompleted
	case publishv1.PublishStatus_PUBLISH_STATUS_FAILED:
		return PublishStatusFailed
	case publishv1.PublishStatus_PUBLISH_STATUS_CANCELLED:
		return PublishStatusCancelled
	default:
		return PublishStatusUnspecified
	}
}
