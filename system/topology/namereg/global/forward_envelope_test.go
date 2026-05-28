// SPDX-License-Identifier: MPL-2.0

package global

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
)

func TestEncodeForwardResponse_RoundTripRegister(t *testing.T) {
	cases := []struct {
		result *RegisterResult
		name   string
	}{
		{
			name: "success",
			result: &RegisterResult{
				PID: pid.PID{Node: "node-1", Host: "h", UniqID: "p1"},
			},
		},
		{
			name: "conflict_with_err",
			result: &RegisterResult{
				Err:         errors.New("global name \"svc\" already registered"),
				ExistingPID: pid.PID{Node: "node-2", Host: "h", UniqID: "p2"},
			},
		},
		{
			name: "resolved",
			result: &RegisterResult{
				PID:         pid.PID{Node: "node-3", Host: "h", UniqID: "p3"},
				ResolvedPID: pid.PID{Node: "node-4", Host: "h", UniqID: "p4"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := encodeFSMResult(CmdRegister, tc.result)
			require.NoError(t, err)
			envelope, err := encodeForwardResponse(42, CmdRegister, "", blob)
			require.NoError(t, err)

			decoded, err := decodeForwardResponse(envelope)
			require.NoError(t, err)
			assert.Equal(t, uint64(42), decoded.CorrID)
			assert.Equal(t, CmdRegister, decoded.CmdKind)
			assert.Empty(t, decoded.ErrMsg)

			rr, ok := decoded.Result.(*RegisterResult)
			require.True(t, ok)
			assert.Equal(t, tc.result.PID, rr.PID)
			assert.Equal(t, tc.result.ExistingPID, rr.ExistingPID)
			assert.Equal(t, tc.result.ResolvedPID, rr.ResolvedPID)
			if tc.result.Err == nil {
				assert.Nil(t, rr.Err)
			} else {
				require.NotNil(t, rr.Err)
				assert.Equal(t, tc.result.Err.Error(), rr.Err.Error())
			}
		})
	}
}

func TestEncodeForwardResponse_RoundTripUnregister(t *testing.T) {
	for _, removed := range []bool{true, false} {
		blob, err := encodeFSMResult(CmdUnregister, &UnregisterResult{Removed: removed})
		require.NoError(t, err)
		envelope, err := encodeForwardResponse(99, CmdUnregister, "", blob)
		require.NoError(t, err)

		decoded, err := decodeForwardResponse(envelope)
		require.NoError(t, err)
		assert.Equal(t, CmdUnregister, decoded.CmdKind)
		ur, ok := decoded.Result.(*UnregisterResult)
		require.True(t, ok)
		assert.Equal(t, removed, ur.Removed)
	}
}

func TestEncodeForwardResponse_RoundTripRemove(t *testing.T) {
	for _, cmd := range []CommandType{CmdRemovePID, CmdRemoveNode} {
		blob, err := encodeFSMResult(cmd, &RemoveResult{Count: 7, HasMore: true})
		require.NoError(t, err)
		envelope, err := encodeForwardResponse(7, cmd, "", blob)
		require.NoError(t, err)

		decoded, err := decodeForwardResponse(envelope)
		require.NoError(t, err)
		assert.Equal(t, cmd, decoded.CmdKind)
		rr, ok := decoded.Result.(*RemoveResult)
		require.True(t, ok)
		assert.Equal(t, 7, rr.Count)
		assert.True(t, rr.HasMore)
	}
}

func TestEncodeForwardResponse_ErrorOnly(t *testing.T) {
	envelope, err := encodeForwardResponse(13, CmdRegister, "leader unavailable", nil)
	require.NoError(t, err)

	decoded, err := decodeForwardResponse(envelope)
	require.NoError(t, err)
	assert.Equal(t, "leader unavailable", decoded.ErrMsg)
	assert.Equal(t, CmdRegister, decoded.CmdKind)
	assert.Nil(t, decoded.Result)
}

func TestDecodeForwardResponse_RejectsHeaderlessEnvelope(t *testing.T) {
	envelope := make([]byte, 8)
	binary.BigEndian.PutUint64(envelope[:8], 1)
	_, err := decodeForwardResponse(envelope)
	require.Error(t, err)
}

func TestDecodeForwardResponse_RejectsBadMagic(t *testing.T) {
	envelope := make([]byte, 8)
	binary.BigEndian.PutUint64(envelope[:8], 2)
	envelope = append(envelope, []byte("not a v1 envelope")...)
	_, err := decodeForwardResponse(envelope)
	require.Error(t, err)
}

func TestDecodeForwardResponse_TruncatedV1Header(t *testing.T) {
	envelope := make([]byte, 9)
	binary.BigEndian.PutUint64(envelope[:8], 3)
	envelope[8] = forwardWireMagic
	_, err := decodeForwardResponse(envelope)
	require.Error(t, err)
}

func TestDecodeForwardResponse_UnknownVersion(t *testing.T) {
	envelope := make([]byte, 10)
	binary.BigEndian.PutUint64(envelope[:8], 4)
	envelope[8] = forwardWireMagic
	envelope[9] = 0xFF
	_, err := decodeForwardResponse(envelope)
	require.Error(t, err)
}

func TestDecodeForwardResponse_UnknownCmdKindInV1(t *testing.T) {
	blob, err := marshalMsgpack(wireRegisterResult{
		PID: pid.PID{Node: "x", Host: "y", UniqID: "z"},
	})
	require.NoError(t, err)
	envelope, err := encodeForwardResponse(5, CommandType(99), "", blob)
	require.NoError(t, err)
	_, err = decodeForwardResponse(envelope)
	require.Error(t, err)
}

func TestEncodeFSMResult_ErrorTypeRejected(t *testing.T) {
	_, err := encodeFSMResult(CmdRegister, errors.New("boom"))
	require.Error(t, err)
}

func TestEncodeFSMResult_NilResult(t *testing.T) {
	out, err := encodeFSMResult(CmdRegister, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestEncodeFSMResult_UnsupportedType(t *testing.T) {
	_, err := encodeFSMResult(CmdRegister, "not a real result")
	require.Error(t, err)
}

func TestForwardEnvelope_TooShort(t *testing.T) {
	_, err := decodeForwardResponse([]byte{0x01, 0x02})
	require.Error(t, err)
}

func TestRecordForwardedApply_NilSafe(t *testing.T) {
	var t0 *telemetry
	t0.recordForwardedApply(CmdRegister, forwardResultOK, time.Second)
}

func TestRecordForwardedApply_LabelsAndHistogram(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordForwardedApply(CmdRegister, forwardResultOK, 12*time.Millisecond)
	tt.recordForwardedApply(CmdRegister, forwardResultError, 7*time.Millisecond)
	tt.recordForwardedApply(CmdUnregister, forwardResultOK, 1*time.Millisecond)

	ok := rec.CounterValue("globalreg_forwarded_apply_total",
		metrics.Labels{"cmd": "register", "result": "ok"})
	assert.Equal(t, float64(1), ok)
	err := rec.CounterValue("globalreg_forwarded_apply_total",
		metrics.Labels{"cmd": "register", "result": "error"})
	assert.Equal(t, float64(1), err)
	unreg := rec.CounterValue("globalreg_forwarded_apply_total",
		metrics.Labels{"cmd": "unregister", "result": "ok"})
	assert.Equal(t, float64(1), unreg)

	histCount := rec.HistogramCount("globalreg_forwarded_apply_latency_seconds",
		metrics.Labels{"cmd": "register"})
	assert.Equal(t, uint64(2), histCount)
}

func TestNewTelemetry_BootstrapForwardedLabels(t *testing.T) {
	_, rec := newTestTelemetry(t)
	for _, cmd := range forwardCommandLabels {
		for _, res := range forwardResultLabels {
			rec.CounterValue("globalreg_forwarded_apply_total",
				metrics.Labels{"cmd": cmd, "result": res})
		}
	}
	histCount := rec.HistogramCount("globalreg_forwarded_apply_latency_seconds",
		metrics.Labels{"cmd": "register"})
	assert.Equal(t, uint64(0), histCount, "bootstrap must not synthesize histogram observations")
}

func TestCommandLabel(t *testing.T) {
	assert.Equal(t, "register", commandLabel(CmdRegister))
	assert.Equal(t, "unregister", commandLabel(CmdUnregister))
	assert.Equal(t, "remove_pid", commandLabel(CmdRemovePID))
	assert.Equal(t, "remove_node", commandLabel(CmdRemoveNode))
	assert.Equal(t, "unknown", commandLabel(CommandType(99)))
}
