// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
)

func TestOutdatedKind(t *testing.T) {
	assert.Equal(t, Kind("pid.outdated"), Outdated)
}

func TestOutdatedEvent_Marshal(t *testing.T) {
	event := OutdatedEvent{
		Sources: []registry.ID{
			registry.NewID("app", "worker"),
			registry.NewID("app", "lib"),
		},
	}

	data, err := json.Marshal(&event)
	require.NoError(t, err)
	assert.Contains(t, string(data), "app:worker")
	assert.Contains(t, string(data), "app:lib")
}

func TestOutdatedPackage(t *testing.T) {
	to := pid.PID{UniqID: "to-pid"}
	sources := []registry.ID{
		registry.NewID("app", "worker"),
		registry.NewID("app", "lib"),
	}

	pkg := OutdatedPackage(to, sources)

	require.NotNil(t, pkg)
	assert.Equal(t, to, pkg.Target)
	assert.Equal(t, "topology", pkg.Source.UniqID)
	require.Len(t, pkg.Messages, 1)
	assert.Equal(t, TopicEvents, pkg.Messages[0].Topic)

	require.Len(t, pkg.Messages[0].Payloads, 1)
	event, ok := pkg.Messages[0].Payloads[0].Data().(*OutdatedEvent)
	require.True(t, ok)
	require.Equal(t, sources, event.Sources)
}

func TestOutdatedPackage_EmptySources(t *testing.T) {
	pkg := OutdatedPackage(pid.PID{UniqID: "to"}, nil)
	require.NotNil(t, pkg)
	require.Len(t, pkg.Messages, 1)
	event, ok := pkg.Messages[0].Payloads[0].Data().(*OutdatedEvent)
	require.True(t, ok)
	assert.Empty(t, event.Sources)
}
