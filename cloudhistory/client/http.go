package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ponyruntime/pony/system/registry/history"
)

// Client implementation
type HTTPCloudHistoryClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPCloudHistoryClient(baseURL string) *HTTPCloudHistoryClient {
	return &HTTPCloudHistoryClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *HTTPCloudHistoryClient) CreateHistoryVersion(ctx context.Context, id string, version *history.CloudVersion) error {
	jsonData, err := json.Marshal(version)
	if err != nil {
		return fmt.Errorf("marshal version: %w", err)
	}

	url := fmt.Sprintf("%s/runtime/%s/history", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *HTTPCloudHistoryClient) GetHistory(ctx context.Context, id string) (history.CloudHistory, error) {
	url := fmt.Sprintf("%s/runtime/%shistory", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var cloudHistory history.CloudHistory
	if err := json.NewDecoder(resp.Body).Decode(&cloudHistory); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return cloudHistory, nil
}
