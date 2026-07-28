// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA25SnapshotDataPayloadBranch(t *testing.T) {
	sourceMap := map[string]any{
		"nested": map[string]any{"value": "before"},
		"bytes":  []byte("abc"),
	}
	source := NewPayload(sourceMap, JSON)

	got, ok := SnapshotData(source).(Payload)
	require.True(t, ok)
	require.NotNil(t, got)
	sourceMap["nested"].(map[string]any)["value"] = "after"
	sourceMap["bytes"].([]byte)[0] = 'z'

	assert.Equal(t, JSON, got.Format())
	snapshotMap := got.Data().(map[string]any)
	assert.Equal(t, "before", snapshotMap["nested"].(map[string]any)["value"])
	assert.Equal(t, []byte("abc"), snapshotMap["bytes"])
	assert.Nil(t, SnapshotData(Payload(nil)))
}

func TestA26SnapshotDataPayloadsBranch(t *testing.T) {
	firstMap := map[string]any{"items": []any{"first"}}
	secondBytes := []byte("second")
	source := Payloads{
		NewPayload(firstMap, YAML),
		nil,
		NewPayload(secondBytes, Bytes),
	}

	got, ok := SnapshotData(source).(Payloads)
	require.True(t, ok)
	firstMap["items"].([]any)[0] = "changed"
	secondBytes[0] = 'X'

	require.Len(t, got, 3)
	assert.Equal(t, YAML, got[0].Format())
	assert.Equal(t, "first", got[0].Data().(map[string]any)["items"].([]any)[0])
	assert.Nil(t, got[1])
	assert.Equal(t, Bytes, got[2].Format())
	assert.Equal(t, []byte("second"), got[2].Data())
}
