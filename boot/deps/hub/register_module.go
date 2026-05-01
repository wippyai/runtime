// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RegisterModuleParams covers the fields the hub's
// POST /api/v1/account/modules endpoint accepts. Only Org/Name/ModuleType
// are required; everything else is optional. Visibility defaults to
// "private" on the server when empty — this matches the wippy CLI default
// of treating new modules as private until the user opts them public.
type RegisterModuleParams struct {
	Org           string   `json:"org_name"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ModuleType    string   `json:"module_type"`
	Visibility    string   `json:"visibility,omitempty"`
	License       string   `json:"license,omitempty"`
	RepositoryURL string   `json:"repository_url,omitempty"`
	HomepageURL   string   `json:"homepage_url,omitempty"`
	Keywords      []string `json:"keywords"`
}

// RegisteredModule is the subset of the hub's REST response we surface to
// the CLI; the full payload includes timestamps, owner ID, etc.
type RegisteredModule struct {
	ID          string `json:"id"`
	OrgName     string `json:"org_name"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	ModuleType  string `json:"module_type"`
	Visibility  string `json:"visibility"`
}

// ErrModuleAlreadyExists is returned when the hub responds 409 to a
// register request — the module is already in the registry.
var ErrModuleAlreadyExists = errors.New("module already exists")

// RegisterModule POSTs to the hub's account-module REST endpoint to create
// the module record before any version is published. The publish flow
// requires the module row to exist in advance; this is the
// one-step-before-publish CLI helper.
//
// Returns ErrModuleAlreadyExists when the hub answers with 409 so callers
// can treat re-runs as idempotent.
func (c *Client) RegisterModule(ctx context.Context, p *RegisterModuleParams) (*RegisteredModule, error) {
	if c.baseURL == "" {
		return nil, errors.New("hub client missing base URL")
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal register-module request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/account/modules", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build register-module request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post register-module: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var out RegisteredModule
		if err := json.Unmarshal(respBody, &out); err != nil {
			return nil, fmt.Errorf("decode register-module response: %w", err)
		}
		return &out, nil
	case http.StatusConflict:
		return nil, ErrModuleAlreadyExists
	default:
		return nil, fmt.Errorf("hub register-module %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
}
