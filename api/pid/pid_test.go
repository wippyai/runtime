package pid

import (
	"testing"

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
