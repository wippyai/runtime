// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
)

var _ HubClient = (*offlineHubClient)(nil)

// offlineHubClient satisfies HubClient without network capability: every method
// reports unavailable verified evidence. Operations that may not reach the Hub
// hold this client instead of the live one, so refusal is structural rather
// than a condition each call site restates.
type offlineHubClient struct{}

func (offlineHubClient) GetManifest(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
	return nil, NewDependencyOfflineError("resolve manifest", org+"/"+module)
}

func (offlineHubClient) ListAllVersions(_ context.Context, org, module string) ([]VersionInfo, error) {
	return nil, NewDependencyOfflineError("list versions", org+"/"+module)
}

func (offlineHubClient) GetDownloadURL(_ context.Context, params *DownloadParams) (*DownloadInfo, error) {
	module := ""
	if params != nil {
		module = params.Org + "/" + params.Module
	}
	return nil, NewDependencyOfflineError("fetch artifact metadata", module)
}

func (offlineHubClient) DownloadToFile(_ context.Context, _, _ string) error {
	return NewDependencyOfflineError("download artifact", "")
}

// hubFor returns the Hub client this operation is entitled to use. An operation
// without DependencyAccessOnline receives a client that cannot reach the Hub.
func (h *DependencyHandler) hubFor(ctx context.Context) HubClient {
	if h == nil || !regapi.DependencyDownloadsAllowed(ctx) {
		return offlineHubClient{}
	}
	return h.hub
}
