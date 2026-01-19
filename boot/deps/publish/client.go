package publish

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wippyai/runtime/cmd/wippy/version"
)

const (
	maxResponseSize = 1 << 20 // 1MB
	clientTimeout   = 5 * time.Minute
)

// PublishStatus represents the status of a publish operation.
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

// Client handles publishing to the registry.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewClient creates a registry publish client.
func NewClient(baseURL, token string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	isLocal := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !isLocal {
		return nil, fmt.Errorf("registry must use HTTPS")
	}

	baseURL = strings.TrimSuffix(baseURL, "/")

	httpClient := &http.Client{
		Timeout: clientTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		token:      token,
	}, nil
}

// PublishParams contains the parameters for publishing.
type PublishParams struct {
	Org          string
	Module       string
	Version      string // semver, mutually exclusive with Label
	Label        string // mutable tag, mutually exclusive with Version
	ReleaseNotes string
	Digest       string
	Size         int64
	Protected    bool
}

// InitResponse contains the response from publish init.
type InitResponse struct {
	PublishID string
	UploadURL string
	ExpiresAt time.Time
}

// StatusResponse contains the publish status.
type StatusResponse struct {
	Status       PublishStatus
	VersionID    string
	ErrorMessage string
}

// moduleName is the nested module name in requests.
type moduleName struct {
	Org  string `json:"org"`
	Name string `json:"name"`
}

// initRequest is the JSON request for InitiatePublish.
type initRequest struct {
	ModuleName        moduleName `json:"moduleName"`
	Version           string     `json:"version,omitempty"`
	Label             string     `json:"label,omitempty"`
	ReleaseNotes      string     `json:"releaseNotes,omitempty"`
	ExpectedSizeBytes uint64     `json:"expectedSizeBytes"`
	Digest            string     `json:"digest"`
	Protected         bool       `json:"protected,omitempty"`
}

// initResponse is the JSON response from InitiatePublish.
type initResponse struct {
	PublishID       string `json:"publishId"`
	UploadURL       string `json:"uploadUrl"`
	UploadExpiresAt string `json:"uploadExpiresAt"`
}

// statusResponse is the JSON response from GetPublishStatus.
type statusResponse struct {
	PublishID    string `json:"publishId"`
	Status       string `json:"status"`
	VersionID    string `json:"versionId,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	StartedAt    string `json:"startedAt,omitempty"`
}

// Init starts a publish operation.
func (c *Client) Init(ctx context.Context, params *PublishParams) (*InitResponse, error) {
	req := &initRequest{
		ModuleName:        moduleName{Org: params.Org, Name: params.Module},
		ReleaseNotes:      params.ReleaseNotes,
		ExpectedSizeBytes: uint64(params.Size),
		Digest:            params.Digest,
		Protected:         params.Protected,
	}

	if params.Version != "" {
		req.Version = params.Version
	} else if params.Label != "" {
		req.Label = params.Label
	} else {
		return nil, fmt.Errorf("either version or label must be specified")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/wippy.registry.module.v1.PublishService/InitiatePublish", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var initResp initResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	expiresAt, _ := time.Parse(time.RFC3339Nano, initResp.UploadExpiresAt)

	return &InitResponse{
		PublishID: initResp.PublishID,
		UploadURL: initResp.UploadURL,
		ExpiresAt: expiresAt,
	}, nil
}

// Upload uploads the pack file to the provided URL.
func (c *Client) Upload(ctx context.Context, uploadURL, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.ContentLength = stat.Size()
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Confirm marks the upload as complete.
func (c *Client) Confirm(ctx context.Context, publishID string) (*StatusResponse, error) {
	body, err := json.Marshal(map[string]string{"publishId": publishID})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/wippy.registry.module.v1.PublishService/ConfirmPublish", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	return &StatusResponse{
		Status: PublishStatusProcessing,
	}, nil
}

// Status retrieves the publish status.
func (c *Client) Status(ctx context.Context, publishID string) (*StatusResponse, error) {
	body, err := json.Marshal(map[string]string{"publishId": publishID})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/wippy.registry.module.v1.PublishService/GetPublishStatus", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var statusResp statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &StatusResponse{
		Status:       parseStatus(statusResp.Status),
		VersionID:    statusResp.VersionID,
		ErrorMessage: statusResp.ErrorMessage,
	}, nil
}

// Cancel cancels a pending publish operation.
func (c *Client) Cancel(ctx context.Context, publishID string) error {
	body, err := json.Marshal(map[string]string{"publishId": publishID})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/wippy.registry.module.v1.PublishService/CancelPublish", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// WaitForCompletion polls until the job is complete or failed.
func (c *Client) WaitForCompletion(ctx context.Context, publishID string, callback func(status *StatusResponse)) (*StatusResponse, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			status, err := c.Status(ctx, publishID)
			if err != nil {
				return nil, err
			}

			if callback != nil {
				callback(status)
			}

			switch status.Status {
			case PublishStatusCompleted:
				return status, nil
			case PublishStatusFailed:
				return status, fmt.Errorf("publish failed: %s", status.ErrorMessage)
			case PublishStatusCancelled:
				return status, fmt.Errorf("publish cancelled")
			}
		}
	}
}

// IsCompleted returns true if the status indicates completion.
func (s *StatusResponse) IsCompleted() bool {
	return s.Status == PublishStatusCompleted
}

// IsFailed returns true if the status indicates failure.
func (s *StatusResponse) IsFailed() bool {
	return s.Status == PublishStatusFailed
}

// StatusString returns a human-readable status.
func (s *StatusResponse) StatusString() string {
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
		return "cancelled"
	default:
		return "unknown"
	}
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "wippy-cli/"+version.Version)
	req.Header.Set("Content-Type", "application/json")
}

func (c *Client) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		return fmt.Errorf("%s: %s", errResp.Code, errResp.Message)
	}
	return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
}

func parseStatus(s string) PublishStatus {
	switch s {
	case "PUBLISH_STATUS_PENDING_UPLOAD":
		return PublishStatusPendingUpload
	case "PUBLISH_STATUS_PROCESSING":
		return PublishStatusProcessing
	case "PUBLISH_STATUS_VALIDATING":
		return PublishStatusValidating
	case "PUBLISH_STATUS_COMPLETED":
		return PublishStatusCompleted
	case "PUBLISH_STATUS_FAILED":
		return PublishStatusFailed
	case "PUBLISH_STATUS_CANCELLED":
		return PublishStatusCancelled
	default:
		return PublishStatusUnspecified
	}
}
