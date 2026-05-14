// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// UploadInput is the input to PublishViaHub: the wapp on disk plus all
// the metadata the hub needs to set up the publish workflow. This is the
// one-call replacement for InitiatePublish + Upload-to-S3 + ConfirmPublish.
type UploadInput struct {
	Org          string
	Module       string
	Version      string
	Label        string
	ReleaseNotes string
	FilePath     string
	Protected    bool
}

// UploadOutput is the publish workflow id the caller polls on.
type UploadOutput struct {
	PublishID string `json:"publish_id"`
}

// PublishViaHub uploads a .wapp file to the hub-mediated upload endpoint and
// returns the publish workflow id. The retry policy is consistent with the
// hub side: jittered exponential backoff on transient network/HTTP errors.
//
// This is the preferred client path because the only hop the CLI makes is
// to the hub itself (server-stable HTTPS). The hub does the second hop to
// S3 with its own retry. Direct-to-S3 PUTs from clients on flaky networks
// — notably Windows winsock against AWS endpoints — get retried *here*,
// not silently fail.
func (c *Client) PublishViaHub(ctx context.Context, in UploadInput) (*UploadOutput, error) {
	if c.token == "" {
		return nil, errors.New("publish requires an authentication token")
	}

	// Load the body once; the hub-mediated handler caps at 100 MB so we
	// can afford to keep it in memory, and a fresh bytes.Reader per
	// retry attempt keeps the op closure idempotent.
	body, err := os.ReadFile(in.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(in.FilePath), err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	size := int64(len(body))

	uploadURL := c.baseURL + "/api/v1/publish/upload"

	var out UploadOutput
	err = retryDo(ctx, DefaultRetryConfig(), func(_ int) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build upload request: %w", err)
		}
		req.ContentLength = size
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Wippy-Org", in.Org)
		req.Header.Set("X-Wippy-Module", in.Module)
		if in.Version != "" {
			req.Header.Set("X-Wippy-Version", in.Version)
		}
		if in.Label != "" {
			req.Header.Set("X-Wippy-Label", in.Label)
		}
		req.Header.Set("X-Wippy-Digest", digest)
		req.Header.Set("X-Wippy-Size", strconv.FormatInt(size, 10))
		if in.Protected {
			req.Header.Set("X-Wippy-Protected", "true")
		}
		if in.ReleaseNotes != "" {
			req.Header.Set("X-Wippy-Release-Notes", in.ReleaseNotes)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
			return &hubStatusError{statusCode: resp.StatusCode, body: string(b)}
		}

		var fresh UploadOutput
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(&fresh); err != nil {
			return fmt.Errorf("decode upload response: %w", err)
		}
		if fresh.PublishID == "" {
			return errors.New("hub returned empty publish_id")
		}
		out = fresh
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadViaHub streams a .wapp from the hub-mediated download endpoint to
// destPath. The retry covers the open (request + status check); once the
// body starts streaming, a failure mid-stream returns an error to the
// caller — HTTP doesn't support un-writing bytes, so partial files are
// cleaned up and the install fails.
func (c *Client) DownloadViaHub(ctx context.Context, digest, destPath string) error {
	if len(digest) != 64 {
		return fmt.Errorf("invalid digest: expected 64-char hex, got %d", len(digest))
	}

	downloadURL := c.baseURL + "/api/v1/wapp/" + digest

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return retryDo(ctx, DefaultRetryConfig(), func(_ int) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return fmt.Errorf("build download request: %w", err)
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
			return &hubStatusError{statusCode: resp.StatusCode, body: string(b)}
		}

		// Write to a temp file first so the destination is only renamed
		// into place once we've successfully copied + synced everything.
		tmp, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".part-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		// Best-effort cleanup; ignored if the rename below succeeds first.
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if _, err := io.Copy(tmp, resp.Body); err != nil {
			tmp.Close()
			// Mid-stream failure: surface as a network error so the
			// retry classifier can decide based on its shape.
			return err
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return fmt.Errorf("sync temp file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close temp file: %w", err)
		}
		return os.Rename(tmpPath, destPath)
	})
}
