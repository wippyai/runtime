// SPDX-License-Identifier: MPL-2.0

package client

import (
	"net/http"

	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"github.com/wippyai/runtime/api/boot"
)

const (
	DefaultRegistryURL = "https://hub.wippy.ai"
)

// NewRegistryClientFromConfig creates a new RegistryClient from boot configuration.
// Config path: modules.registry_url
// Default: DefaultRegistryURL
func NewRegistryClientFromConfig(cfg boot.Config) *RegistryClient {
	baseURL := DefaultRegistryURL

	if cfg != nil {
		modulesCfg := cfg.Sub("modules")
		if modulesCfg != nil {
			baseURL = modulesCfg.GetString("registry_url", DefaultRegistryURL)
		}
	}

	httpClient := &http.Client{}

	return NewRegistryClient(
		identityv1connect.NewOrganizationServiceClient(httpClient, baseURL),
		modulev1connect.NewModuleServiceClient(httpClient, baseURL),
		modulev1connect.NewLabelServiceClient(httpClient, baseURL),
		modulev1connect.NewDownloadServiceClient(httpClient, baseURL),
	)
}
