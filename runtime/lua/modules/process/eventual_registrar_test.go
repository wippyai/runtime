// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestEventualServiceBoundInContextSatisfiesLuaRegistrar(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	svc := eventual.NewService(eventual.Config{LocalNodeID: "node-1"})
	ctx = topology.WithEventualRegistry(ctx, svc)

	require.NotNil(t, topology.GetEventualRegistry(ctx))
	require.NotNil(t, getEventualRegistrar(ctx))
}
