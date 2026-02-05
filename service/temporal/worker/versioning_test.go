package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/sdk/workflow"
)

func TestMapVersioningBehavior(t *testing.T) {
	assert.Equal(t, workflow.VersioningBehaviorPinned, mapVersioningBehavior(api.VersioningBehaviorPinned))
	assert.Equal(t, workflow.VersioningBehaviorAutoUpgrade, mapVersioningBehavior(api.VersioningBehaviorAutoUpgrade))
	assert.Equal(t, workflow.VersioningBehaviorUnspecified, mapVersioningBehavior(api.VersioningBehavior("unknown")))
}
