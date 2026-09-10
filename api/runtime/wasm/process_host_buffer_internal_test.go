package wasm

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestProcessHostBufferLimitDecoding(t *testing.T) {
	for _, value := range []string{"0", "1", "131072", "9007199254740991"} {
		t.Run(value, func(t *testing.T) {
			raw := fmt.Sprintf(`{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:abcd","method":"run","meta":{"options":{"limits":{"host_buffer_bytes":%s}}}}`, value)
			var cfg ProcessConfig
			require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
			require.NoError(t, cfg.Validate())
			var expected int64
			require.NoError(t, json.Unmarshal([]byte(value), &expected))
			require.Equal(t, expected, cfg.Limits().HostBufferBytes)
			cfg.SetOptions(cfg.Options())
			for _, yamlMode := range []bool{false, true} {
				var data []byte
				var err error
				if yamlMode {
					data, err = yaml.Marshal(cfg)
				} else {
					data, err = json.Marshal(cfg)
				}
				require.NoError(t, err)
				var round ProcessConfig
				if yamlMode {
					err = yaml.Unmarshal(data, &round)
				} else {
					err = json.Unmarshal(data, &round)
				}
				require.NoError(t, err)
				require.NoError(t, round.Validate())
				require.Equal(t, expected, round.Limits().HostBufferBytes)
			}
		})
	}
	for _, value := range []string{"-1", "1.5", `"131072"`, "true", "9007199254740992", "9223372036854775807", "9223372036854775808"} {
		t.Run("reject-"+value, func(t *testing.T) {
			var cfg ProcessConfig
			raw := fmt.Sprintf(`{"fs":"app.fs:code","path":"/actor.wasm","hash":"sha256:abcd","method":"run","meta":{"options":{"limits":{"host_buffer_bytes":%s}}}}`, value)
			err := json.Unmarshal([]byte(raw), &cfg)
			if err == nil {
				err = cfg.Validate()
			}
			require.Error(t, err)
		})
	}
	var cfg ProcessConfig
	require.NoError(t, json.Unmarshal([]byte(validProcessJSON()), &cfg))
	require.NoError(t, cfg.Validate())
	require.Zero(t, cfg.Limits().HostBufferBytes)
	_, err := validateOptionsStruct(ProcessOptions{Limits: ProcessLimitsConfig{HostBufferBytes: -1}})
	require.ErrorIs(t, err, ErrProcessHostBufferBytesInvalid)
}
