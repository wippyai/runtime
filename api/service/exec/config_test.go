// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestDockerExecutorConfig_OwnershipLabelBounds(t *testing.T) {
	for name, labels := range map[string]map[string]string{
		"empty label":    {"": "OWNER"},
		"label NUL":      {"owner\x00": "OWNER"},
		"label too long": {strings.Repeat("x", 257): "OWNER"},
		"empty source":   {"owner": ""},
		"source equals":  {"owner": "OWNER=OTHER"},
		"source NUL":     {"owner": "OWNER\x00"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &DockerExecutorConfig{Image: "fixture", LabelsFromEnv: labels}
			err := cfg.Validate()
			require.Error(t, err)
			var invalid apierror.Error
			require.ErrorAs(t, err, &invalid)
			require.Equal(t, apierror.Invalid, invalid.Kind())
			require.Equal(t, apierror.False, invalid.Retryable())
		})
	}
	labels := make(map[string]string, 65)
	for i := range 64 {
		labels[fmt.Sprintf("owner-%d", i)] = "OWNER"
	}
	cfg := &DockerExecutorConfig{Image: "fixture", LabelsFromEnv: labels}
	require.NoError(t, cfg.Validate())
	labels["extra"] = "OWNER"
	require.Error(t, cfg.Validate())
	cfg.LabelsFromEnv = map[string]string{strings.Repeat("x", 256): "OWNER"}
	require.NoError(t, cfg.Validate())
}
