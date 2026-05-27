// SPDX-License-Identifier: MPL-2.0

package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/auth"
	"github.com/wippyai/runtime/api/version"
)

const (
	maxResponseSize = 1 << 20 // 1MB
	clientTimeout   = 30 * time.Second
)

// Client validates tokens against the registry API.
type Client struct {
	http    *http.Client
	baseURL string
}

// NewClient creates a registry auth client.
func NewClient(baseURL string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	// Require HTTPS for non-local hosts. host.docker.internal /
	// host.containers.internal only ever resolve to the host machine, so
	// plaintext across them is no riskier than localhost.
	host := u.Hostname()
	isLocal := host == "localhost" || host == "127.0.0.1" ||
		host == "host.docker.internal" || host == "host.containers.internal"
	if u.Scheme != "https" && !isLocal {
		return nil, fmt.Errorf("registry must use HTTPS")
	}

	baseURL = strings.TrimSuffix(baseURL, "/")

	return &Client{
		http: &http.Client{
			Timeout: clientTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
		},
		baseURL: baseURL,
	}, nil
}

// ValidateResult contains validation response data.
type ValidateResult struct {
	Orgs []OrgInfo
}

// OrgInfo contains organization membership.
type OrgInfo struct {
	ID          string
	Name        string
	DisplayName string
	Role        string
}

// Validate checks if the token is valid and returns account info.
func (c *Client) Validate(ctx context.Context, token string) (*ValidateResult, error) {
	if err := ValidateTokenFormat(token); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/account/orgs", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wippy-cli/"+version.Version)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Bounded read
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return parseOrgsResponse(body)
	case http.StatusUnauthorized:
		return nil, auth.ErrTokenInvalid
	case http.StatusForbidden:
		return nil, auth.ErrInsufficientScope
	default:
		return nil, fmt.Errorf("server error: %d", resp.StatusCode)
	}
}

// ValidateTokenFormat checks if the token has valid format.
func ValidateTokenFormat(token string) error {
	if token == "" {
		return fmt.Errorf("token is empty")
	}
	if !strings.HasPrefix(token, TokenPrefix) {
		return fmt.Errorf("token must start with '%s'", TokenPrefix)
	}
	if len(token) < TokenMinLength {
		return fmt.Errorf("token too short")
	}
	return nil
}

type orgResponse struct {
	Org struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	} `json:"org"`
	Role string `json:"role"`
}

func parseOrgsResponse(body []byte) (*ValidateResult, error) {
	var orgs []orgResponse
	if err := json.Unmarshal(body, &orgs); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	result := &ValidateResult{
		Orgs: make([]OrgInfo, len(orgs)),
	}
	for i, o := range orgs {
		result.Orgs[i] = OrgInfo{
			ID:          o.Org.ID,
			Name:        o.Org.Name,
			DisplayName: o.Org.DisplayName,
			Role:        o.Role,
		}
	}

	return result, nil
}
