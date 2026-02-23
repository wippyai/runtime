// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestNewInvalidHostTypeError(t *testing.T) {
	err := NewInvalidHostTypeError("host1", "node1")
	assert.Contains(t, err.Error(), "invalid host type")
	assert.Equal(t, "Internal", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	hostID, _ := details.Get("host_id")
	assert.Equal(t, "host1", hostID)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewSubscriberError(t *testing.T) {
	cause := errors.New("subscriber error")
	err := NewSubscriberError(cause)
	assert.Contains(t, err.Error(), "failed to create subscriber")
	assert.True(t, err.Retryable().Bool())
	assert.True(t, errors.Is(err, cause))
	details := err.Details()
	require.NotNil(t, details)
	causeValue, _ := details.Get("cause")
	assert.Equal(t, "subscriber error", causeValue)
}

func TestNewHostExistsError(t *testing.T) {
	err := NewHostExistsError("host1", "node1")
	assert.Contains(t, err.Error(), "already exists")
	assert.Equal(t, "AlreadyExists", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	hostID, _ := details.Get("host_id")
	assert.Equal(t, "host1", hostID)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewHostNotFoundError(t *testing.T) {
	err := NewHostNotFoundError("host1", "node1")
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, "NotFound", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	hostID, _ := details.Get("host_id")
	assert.Equal(t, "host1", hostID)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewExternalNodeError(t *testing.T) {
	err := NewExternalNodeError("node1")
	assert.Contains(t, err.Error(), "cannot route to external node")
	assert.Equal(t, "Unavailable", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewNodeNotFoundError(t *testing.T) {
	err := NewNodeNotFoundError("node1")
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, "NotFound", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewHostNotAttachableError(t *testing.T) {
	err := NewHostNotAttachableError("host1")
	assert.Contains(t, err.Error(), "does not support attachment")
	assert.Equal(t, "Invalid", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	hostID, _ := details.Get("host_id")
	assert.Equal(t, "host1", hostID)
}

func TestNewPeerExistsError(t *testing.T) {
	err := NewPeerExistsError("node1")
	assert.Contains(t, err.Error(), "peer node already registered")
	assert.Equal(t, "AlreadyExists", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewPeerConflictError(t *testing.T) {
	err := NewPeerConflictError("node1")
	assert.Contains(t, err.Error(), "conflicts with local node")
	assert.Equal(t, "Conflict", err.Kind().String())
	details := err.Details()
	require.NotNil(t, details)
	nodeID, _ := details.Get("node_id")
	assert.Equal(t, "node1", nodeID)
}

func TestNewAlreadyAttachedError(t *testing.T) {
	p := pid.PID{Host: "host1", UniqID: "proc1"}
	err := NewAlreadyAttachedError(p)
	assert.Contains(t, err.Error(), "already attached")
	assert.True(t, errors.Is(err, ErrAlreadyAttached))
	details := err.Details()
	require.NotNil(t, details)
	pidValue, _ := details.Get("pid")
	assert.Equal(t, p.String(), pidValue)
}
