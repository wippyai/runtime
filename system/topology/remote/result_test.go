// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
)

func TestCompletionResultRoundTrip(t *testing.T) {
	cases := []*runtime.Result{
		{},
		{Value: payload.NewString("finished")},
		{Value: payload.NewString("")},
		{Value: payload.NewPayload([]byte(nil), payload.Bytes)},
		{Value: payload.NewPayload(`{"ok":true}`, payload.JSON)},
		{Value: payload.NewPayload([]byte{0, 255, 1}, payload.Bytes)},
		{Value: payload.NewPayload([]byte(`{"answer":42}`), payload.JSON)},
		{Error: errors.New("target failed")},
		{Value: payload.NewString("partial"), Error: errors.New("failed after output")},
		{Value: payload.NewString(strings.Repeat("x", MaxResultValueBytes)), Error: errors.New(strings.Repeat("\x00", MaxResultErrorBytes))},
	}
	for _, result := range cases {
		captured, err := CaptureResult(result)
		require.NoError(t, err)
		encoded, err := EncodeResult(captured)
		require.NoError(t, err)
		decoded, err := DecodeResult(encoded)
		require.NoError(t, err)
		require.Equal(t, captured, decoded)
	}
}

func TestCompletionResultUnavailableIsExplicit(t *testing.T) {
	cases := []struct {
		value  payload.Payload
		reason string
	}{
		{payload.New(make(chan int)), "unsupported_format"},
		{payload.NewPayload("wrong", payload.Bytes), "invalid_value"},
		{payload.NewPayload([]byte(`{`), payload.JSON), "invalid_value"},
		{payload.NewString(string([]byte{255})), "invalid_value"},
		{payload.NewString(strings.Repeat("a", MaxResultValueBytes+1)), "value_too_large"},
	}
	for _, test := range cases {
		result, err := CaptureResult(&runtime.Result{Value: test.value})
		require.NoError(t, err)
		require.Equal(t, "unavailable", result.ValueState)
		require.Equal(t, test.reason, result.Reason)
		encoded, err := EncodeResult(result)
		require.NoError(t, err)
		decoded, err := DecodeResult(encoded)
		require.NoError(t, err)
		require.Equal(t, result, decoded)
	}
	result, err := CaptureResult(&runtime.Result{Error: errors.New(strings.Repeat("e", MaxResultErrorBytes+1))})
	require.NoError(t, err)
	require.Equal(t, "unavailable", result.ErrorState)
	require.Empty(t, result.ErrorText)
	_, err = CaptureResult(nil)
	require.ErrorIs(t, err, ErrInvalidResult)
}

func TestCompletionResultCopiesAndRejectsMalformedWire(t *testing.T) {
	source := []byte("owned")
	result, err := CaptureResult(&runtime.Result{Value: payload.NewPayload(source, payload.Bytes)})
	require.NoError(t, err)
	source[0] = 'X'
	require.Equal(t, []byte("owned"), result.Data)
	encoded, err := EncodeResult(result)
	require.NoError(t, err)
	invalid := [][]byte{
		bytes.Replace(encoded, []byte(`"data":"b3duZWQ="`), []byte(`"data":null`), 1),
		bytes.Replace(encoded, []byte(`"data":"b3duZWQ="`), []byte(`"data":[1,2]`), 1),
		append(bytes.Clone(encoded), []byte(` {}`)...),
		bytes.Replace(encoded, []byte(`"format":`), []byte(`"extra":0,"format":`), 1),
		bytes.Replace(encoded, []byte(`"format":`), []byte(`"format":"","format":`), 1),
		bytes.Replace(encoded, []byte(`"reason":""`), []byte(`"reason":null`), 1),
		bytes.Replace(encoded, []byte(`"reason":"",`), nil, 1),
		bytes.Replace(encoded, []byte(`"value_state":"present"`), []byte(`"value_state":"absent"`), 1),
		bytes.Repeat([]byte(" "), MaxResultWireBytes+1),
	}
	for _, data := range invalid {
		_, err := DecodeResult(data)
		require.ErrorIs(t, err, ErrInvalidResult)
	}
}

func FuzzCompletionResult(f *testing.F) {
	result, _ := CaptureResult(&runtime.Result{Value: payload.NewString("done")})
	encoded, _ := EncodeResult(result)
	f.Add(encoded)
	f.Add([]byte(`{"data":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := DecodeResult(data)
		if err != nil {
			return
		}
		encoded, err := EncodeResult(decoded)
		require.NoError(t, err)
		again, err := DecodeResult(encoded)
		require.NoError(t, err)
		require.Equal(t, decoded, again)
	})
}
