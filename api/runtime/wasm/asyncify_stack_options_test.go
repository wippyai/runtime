// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"encoding/json"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
)

func TestAsyncifyStackOptionsAdmission(t *testing.T) {
	for _, value := range []any{int64(-1), uint64(4294967288), 1.5, "65536"} {
		meta := attrs.Bag{"options": map[string]any{"limits": map[string]any{"asyncify_stack_bytes": value}}}
		_, err := parseAndValidateOptions(meta)
		require.Error(t, err, "actor must reject %v", value)
		_, err = parseAndValidateFunctionOptions(meta)
		require.Error(t, err, "function must reject %v", value)
	}
	meta := attrs.Bag{"options": map[string]any{"limits": map[string]any{"asyncify_stack_bytes": 65536}}}
	processOpts, err := parseAndValidateOptions(meta)
	require.NoError(t, err)
	var process ProcessConfig
	process.SetOptions(processOpts)
	require.Equal(t, uint32(65536), process.EffectiveLimitsConfig().AsyncifyStackBytes)
	functionOpts, err := parseAndValidateFunctionOptions(meta)
	require.NoError(t, err)
	require.Equal(t, uint32(65536), functionOpts.Limits.AsyncifyStackBytes)
	// Serialization must retain the budget when normalizing legacy metadata.
	again, err := parseAndValidateOptions(attrs.Bag{"options": serializeProcessOptions(processOpts)})
	require.NoError(t, err)
	require.Equal(t, processOpts.Limits.AsyncifyStackBytes, again.Limits.AsyncifyStackBytes)
	againFunction, err := parseAndValidateFunctionOptions(attrs.Bag{"options": serializeFunctionOptions(functionOpts)})
	require.NoError(t, err)
	require.Equal(t, functionOpts.Limits.AsyncifyStackBytes, againFunction.Limits.AsyncifyStackBytes)
}

func TestAsyncifyStackLimitsEncoding(t *testing.T) {
	limits := LimitsConfig{AsyncifyStackBytes: 65536}
	encoded, err := json.Marshal(limits)
	require.NoError(t, err)
	var fromJSON LimitsConfig
	require.NoError(t, json.Unmarshal(encoded, &fromJSON))
	require.Equal(t, limits.AsyncifyStackBytes, fromJSON.AsyncifyStackBytes)
	encoded, err = yaml.Marshal(limits)
	require.NoError(t, err)
	var fromYAML LimitsConfig
	require.NoError(t, yaml.Unmarshal(encoded, &fromYAML))
	require.Equal(t, limits.AsyncifyStackBytes, fromYAML.AsyncifyStackBytes)
	require.ErrorIs(t, (LimitsConfig{AsyncifyStackBytes: ^uint32(0)}).Validate(), ErrInvalidAsyncifyStackBytes)
	_, err = validateOptionsStruct(ProcessOptions{Limits: ProcessLimitsConfig{AsyncifyStackBytes: ^uint32(0)}})
	require.ErrorIs(t, err, ErrInvalidAsyncifyStackBytes)
}
