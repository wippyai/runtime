// SPDX-License-Identifier: MPL-2.0

package httpclient

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestOverlayNetworkRequestOptionsTypecheck(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(`
local client = require("http_client")
local function network(opts: client.RequestOptions): string?
    return opts.overlay_network
end
local response, err = client.get("http://peer/", {overlay_network = "app:network"})
local response2, err2 = client.request("POST", "http://peer/", {overlay_network = "app:network", body = "test"})
`, "overlay_request.lua", map[string]*io.Manifest{"http_client": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}
