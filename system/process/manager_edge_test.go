// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	apiprocess "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

type countingNode struct {
	*mockNode
	getHostCalls int
}

func (n *countingNode) GetHost(id pid.HostID) (relay.Receiver, bool) {
	n.getHostCalls++
	return n.mockNode.GetHost(id)
}

func TestY08ManagerRejectsNilStart(t *testing.T) {
	node := &countingNode{mockNode: newMockNode()}
	manager := NewManager(node, zap.NewNop())

	got, err := manager.Start(context.Background(), nil)

	require.Equal(t, pid.PID{}, got)
	var typed apierror.Error
	require.True(t, errors.As(err, &typed))
	require.Equal(t, apiprocess.InvalidState, typed.Kind())
	require.Zero(t, node.getHostCalls)
}

func TestY09ManagerRejectsNilNode(t *testing.T) {
	manager := NewManager(nil, zap.NewNop())

	got, err := manager.Start(context.Background(), &apiprocess.Start{
		HostID: "host",
		Source: registry.ParseID("test:source"),
	})

	require.Equal(t, pid.PID{}, got)
	var typed apierror.Error
	require.True(t, errors.As(err, &typed))
	require.Equal(t, apiprocess.InvalidState, typed.Kind())
}
