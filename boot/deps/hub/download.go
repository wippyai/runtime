// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	downloadv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/backoff"
)

const (
	downloadMaxAttempts = 3
)

type DownloadInfo struct {
	ExpiresAt time.Time
	URL       string
	Digest    string
	Version   string
	Size      uint64
	Protected bool
}

type DownloadParams struct {
	Org       string
	Module    string
	ModuleID  string
	Version   string
	VersionID string
	Label     string
}

func (c *Client) GetDownloadURL(ctx context.Context, params *DownloadParams) (*DownloadInfo, error) {
	req := &downloadv1.GetDownloadURLRequest{
		Module: &modulev1.ModuleRef{},
	}
	if params.ModuleID != "" {
		req.Module.Value = &modulev1.ModuleRef_Id{Id: params.ModuleID}
	} else {
		req.Module.Value = &modulev1.ModuleRef_Name{
			Name: &modulev1.ModuleName{
				Org:  params.Org,
				Name: params.Module,
			},
		}
	}

	if params.VersionID != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Id{
				Id: params.VersionID,
			},
		}
	} else if params.Version != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Version{
				Version: params.Version,
			},
		}
	} else if params.Label != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Label{
				Label: params.Label,
			},
		}
	}

	resp, err := c.Download.GetDownloadURL(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	info := &DownloadInfo{
		Version:   resp.Msg.Version,
		Protected: resp.Msg.Protected,
	}

	if resp.Msg.Download != nil {
		info.URL = resp.Msg.Download.Url
		info.Digest = resp.Msg.Download.Digest
		info.Size = resp.Msg.Download.SizeBytes
		if resp.Msg.Download.ExpiresAt != nil {
			info.ExpiresAt = resp.Msg.Download.ExpiresAt.AsTime()
		}
	}

	return info, nil
}

func (c *Client) DownloadToFile(ctx context.Context, url, destPath string) error {
	// Retry transient download failures with bounded exponential backoff.
	retry := backoff.NewCalculator(supervisor.RetryPolicy{
		MaxAttempts:   downloadMaxAttempts - 1, // intervals between attempts
		InitialDelay:  150 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      time.Second,
		Jitter:        0.2,
	})

	for attempt := 1; attempt <= downloadMaxAttempts; attempt++ {
		err := c.downloadToFileOnce(ctx, url, destPath)
		if err == nil {
			return nil
		}

		if !isRetriableDownloadError(err) || attempt == downloadMaxAttempts {
			return err
		}

		wait := retry.NextInterval()
		if wait <= 0 {
			return err
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}

	return fmt.Errorf("download failed after retries")
}

func (c *Client) downloadToFileOnce(ctx context.Context, url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &downloadRequestError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return &downloadStatusError{
			statusCode: resp.StatusCode,
			body:       string(body),
		}
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(destPath)
		return fmt.Errorf("sync file: %w", err)
	}

	return f.Close()
}

type downloadRequestError struct {
	err error
}

func (e *downloadRequestError) Error() string {
	return fmt.Sprintf("download failed: %v", e.err)
}

func (e *downloadRequestError) Unwrap() error {
	return e.err
}

type downloadStatusError struct {
	body       string
	statusCode int
}

func (e *downloadStatusError) Error() string {
	return fmt.Sprintf("download failed with status %d: %s", e.statusCode, e.body)
}

func isRetriableDownloadError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var statusErr *downloadStatusError
	if errors.As(err, &statusErr) {
		code := statusErr.statusCode
		return code == http.StatusRequestTimeout ||
			code == http.StatusTooManyRequests ||
			code >= http.StatusInternalServerError
	}

	var reqErr *downloadRequestError
	if errors.As(err, &reqErr) {
		var netErr net.Error
		if errors.As(reqErr, &netErr) {
			return true
		}
		return true
	}

	return false
}
