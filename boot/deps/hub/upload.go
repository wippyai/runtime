package hub

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

const maxResponseSize = 1 << 20

func uploadFile(httpClient *http.Client, uploadURL, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, uploadURL, f)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.ContentLength = stat.Size()
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := httpClient.Do(req)
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
