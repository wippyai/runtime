// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestartIntensityWireRoundTrip(t *testing.T) {
	var policy RetryPolicy
	require.NoError(t, json.Unmarshal([]byte(`{"intensity":{"max_restarts":3,"window":"20s"}}`), &policy))
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.Equal(t, map[string]any{"max_restarts": float64(3), "window": "20s"}, got["intensity"])
}

func TestRestartIntensityRejectsInvalidWireValues(t *testing.T) {
	for _, raw := range []string{
		`{"intensity":{"max_restarts":0,"window":"20s"}}`,
		`{"intensity":{"max_restarts":-1,"window":"20s"}}`,
		`{"intensity":{"max_restarts":1,"window":"0s"}}`,
		`{"intensity":{"max_restarts":1,"window":"-1s"}}`,
		`{"intensity":{"max_restarts":1,"window":"invalid"}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var policy RetryPolicy
			require.Error(t, json.Unmarshal([]byte(raw), &policy))
		})
	}
}
