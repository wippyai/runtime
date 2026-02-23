// SPDX-License-Identifier: MPL-2.0

package pid

import (
	"encoding/json"
	"errors"
	"testing"

	apierror "github.com/wippyai/runtime/api/error"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPID_String(t *testing.T) {
	t.Run("with node", func(t *testing.T) {
		pid := PID{Node: "node1", Host: "host1", UniqID: "proc1"}
		expected := "{node1@host1|proc1}"
		assert.Equal(t, expected, pid.String())
	})

	t.Run("without node", func(t *testing.T) {
		pid := PID{Host: "host1", UniqID: "proc1"}
		expected := "{host1|proc1}"
		assert.Equal(t, expected, pid.String())
	})

	t.Run("uses cached value", func(t *testing.T) {
		pid := PID{Host: "host1", UniqID: "proc1", cachedString: "{cached}"}
		assert.Equal(t, "{cached}", pid.String())
	})
}

func TestPID_Precomputed(t *testing.T) {
	pid := PID{Node: "node1", Host: "host1", UniqID: "proc1"}
	computed := pid.Precomputed()

	assert.Equal(t, pid.Node, computed.Node)
	assert.Equal(t, pid.Host, computed.Host)
	assert.Equal(t, pid.UniqID, computed.UniqID)
	assert.NotEmpty(t, computed.cachedString)
	assert.Equal(t, "{node1@host1|proc1}", computed.cachedString)
}

func TestParsePID(t *testing.T) {
	t.Run("with node", func(t *testing.T) {
		pid, err := ParsePID("{node1@host1|proc1}")
		require.NoError(t, err)
		assert.Equal(t, "node1", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
		assert.NotEmpty(t, pid.cachedString)
	})

	t.Run("without node", func(t *testing.T) {
		pid, err := ParsePID("{host1|proc1}")
		require.NoError(t, err)
		assert.Equal(t, "", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
	})

	t.Run("old 3-part format", func(t *testing.T) {
		pid, err := ParsePID("{node1@host1|ns:name|proc1}")
		require.NoError(t, err)
		assert.Equal(t, "node1", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
	})

	t.Run("missing braces", func(t *testing.T) {
		_, err := ParsePID("host1|proc1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing opening brace", func(t *testing.T) {
		_, err := ParsePID("host1|proc1}")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing closing brace", func(t *testing.T) {
		_, err := ParsePID("{host1|proc1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing pipe", func(t *testing.T) {
		_, err := ParsePID("{host1proc1}")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing pipe")
	})

	t.Run("too short", func(t *testing.T) {
		_, err := ParsePID("{}")
		require.Error(t, err)
	})
}

func TestPID_MarshalJSON(t *testing.T) {
	t.Run("with node", func(t *testing.T) {
		pid := PID{Node: "node1", Host: "host1", UniqID: "proc1"}
		data, err := json.Marshal(&pid)
		require.NoError(t, err)
		assert.Equal(t, `"{node1@host1|proc1}"`, string(data))
	})

	t.Run("without node", func(t *testing.T) {
		pid := PID{Host: "host1", UniqID: "proc1"}
		data, err := json.Marshal(&pid)
		require.NoError(t, err)
		assert.Equal(t, `"{host1|proc1}"`, string(data))
	})
}

func TestPID_UnmarshalJSON(t *testing.T) {
	t.Run("with node", func(t *testing.T) {
		var pid PID
		err := json.Unmarshal([]byte(`"{node1@host1|proc1}"`), &pid)
		require.NoError(t, err)
		assert.Equal(t, "node1", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
	})

	t.Run("without node", func(t *testing.T) {
		var pid PID
		err := json.Unmarshal([]byte(`"{host1|proc1}"`), &pid)
		require.NoError(t, err)
		assert.Equal(t, "", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
	})

	t.Run("too short", func(t *testing.T) {
		var pid PID
		err := json.Unmarshal([]byte(`"{}"`), &pid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing quotes", func(t *testing.T) {
		var pid PID
		err := pid.UnmarshalJSON([]byte(`{host1|proc1}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing quotes")
	})
}

func TestPID_JSONRoundTrip(t *testing.T) {
	original := PID{Node: "node1", Host: "host1", UniqID: "proc1"}

	data, err := json.Marshal(&original)
	require.NoError(t, err)

	var parsed PID
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, original.Node, parsed.Node)
	assert.Equal(t, original.Host, parsed.Host)
	assert.Equal(t, original.UniqID, parsed.UniqID)
}

func TestErrorInterface(t *testing.T) {
	t.Run("ErrInvalidPIDFormat", func(t *testing.T) {
		err := ErrInvalidPIDFormat
		assert.Equal(t, "invalid pid format", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
		assert.False(t, err.Retryable().Bool())
		assert.Nil(t, err.Details())
		assert.True(t, errors.Is(err, ErrInvalidPIDFormat))
	})

	t.Run("SetMessage", func(t *testing.T) {
		err := apierror.SetMessage(ErrInvalidPIDFormat, "custom message")
		assert.Equal(t, "custom message", err.Error())
		assert.Equal(t, ErrInvalidPIDFormat.Kind(), err.Kind())
		assert.Equal(t, ErrInvalidPIDFormat.Retryable(), err.Retryable())
	})
}
