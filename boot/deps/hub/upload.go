// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"io"
	"net/http"
	"os"
)

const maxResponseSize = 1 << 20

func uploadFile(ctx context.Context, httpClient *http.Client, uploadURL, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return NewArtifactIOError("open file", filePath, err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return NewArtifactIOError("stat file", filePath, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
	if err != nil {
		return NewHubRequestError("create upload request", err)
	}

	req.ContentLength = stat.Size()
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		return NewHubRequestError("upload file", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return NewHubResponseError("upload file", resp.StatusCode, string(body))
	}

	return nil
}
