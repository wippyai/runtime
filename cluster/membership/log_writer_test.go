// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestMemberlistLogWriter_DowngradesExpectedNetworkFailures(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	writer := newMemberlistLogWriter(zap.New(core), false)

	lines := []string{
		"2026/06/05 12:00:00 [ERR] memberlist: Failed fallback TCP ping: timeout 1s\n",
		"2026/06/05 12:00:00 [ERR] memberlist: Failed to send UDP ping: sendto: operation not permitted\n",
		"2026/06/05 12:00:00 [ERR] memberlist: Failed to send gossip to 10.0.0.2:7946: i/o timeout\n",
	}
	for _, line := range lines {
		_, err := writer.Write([]byte(line))
		require.NoError(t, err)
	}

	require.Len(t, logs.FilterLevelExact(zapcore.ErrorLevel).All(), 0)
	require.Len(t, logs.FilterLevelExact(zapcore.WarnLevel).All(), len(lines))
}

func TestMemberlistLogWriter_KeepsProtocolErrorsAtError(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	writer := newMemberlistLogWriter(zap.New(core), false)

	_, err := writer.Write([]byte("2026/06/05 12:00:00 [ERR] memberlist: Failed to decode user message: short buffer\n"))
	require.NoError(t, err)

	require.Len(t, logs.FilterLevelExact(zapcore.ErrorLevel).All(), 1)
	require.Len(t, logs.FilterLevelExact(zapcore.WarnLevel).All(), 0)
}

func TestMemberlistLogWriter_PartialLineAndWrongSeverity(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	writer := newMemberlistLogWriter(zap.New(core), true)

	_, err := writer.Write([]byte("2026/06/05 12:00:00 [WARN] member"))
	require.NoError(t, err)
	require.Len(t, logs.All(), 0)

	_, err = writer.Write([]byte("list: suspect node\n"))
	require.NoError(t, err)
	require.Len(t, logs.FilterLevelExact(zapcore.WarnLevel).All(), 1)

	_, err = writer.Write([]byte("2026/06/05 12:00:00 [BOGUS] memberlist: unknown severity\n"))
	require.NoError(t, err)
	require.Len(t, logs.FilterLevelExact(zapcore.InfoLevel).All(), 1)
}
