// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/cluster/internode"
)

func completionFixture(t *testing.T) Completion {
	t.Helper()
	result, err := CaptureResult(&runtime.Result{Value: payload.NewString("actual result")})
	require.NoError(t, err)
	return Completion{Monitor: testControl(), Result: result}
}

func TestCompletionNativeCodecRoundTrip(t *testing.T) {
	original := completionFixture(t)
	pkg, err := EncodeCompletion(original)
	require.NoError(t, err)
	codec := internode.NewMessageCodec(nil)
	encoded, err := codec.Encode(pkg)
	relay.ReleasePackage(pkg)
	require.NoError(t, err)
	decoded, err := codec.Decode(encoded)
	require.NoError(t, err)
	defer relay.ReleasePackage(decoded)
	protectControl(decoded)
	result, err := DecodeCompletion(decoded, original.Monitor.Watcher.Node)
	require.NoError(t, err)
	require.Equal(t, original.Result, result.Result)
	require.True(t, samePID(original.Monitor.Watcher, result.Monitor.Watcher))
	require.True(t, samePID(original.Monitor.Target, result.Monitor.Target))
	require.Equal(t, original.Monitor.RequestID, result.Monitor.RequestID)
	require.Equal(t, original.Monitor.Grant, result.Monitor.Grant)
	_, err = DecodeControl(decoded, original.Monitor.Watcher.Node)
	require.Error(t, err)
}

func TestCompletionRejectsInvalidEnvelope(t *testing.T) {
	cases := map[string]func(*relay.Package){
		"unprotected":       func(p *relay.Package) { p.Ingress.IntegrityProtected = false },
		"unauthenticated":   func(p *relay.Package) { p.Ingress.Authenticated = false },
		"wrong peer":        func(p *relay.Package) { p.Ingress.Node = "other" },
		"actor spoof":       func(p *relay.Package) { p.Source.UniqID = "actor" },
		"wrong destination": func(p *relay.Package) { p.Target.UniqID = "actor" },
		"wrong topic":       func(p *relay.Package) { p.Messages[0].Topic = ControlTopic },
		"missing result":    func(p *relay.Package) { p.Messages[0].Payloads = p.Messages[0].Payloads[:1] },
		"extra payload": func(p *relay.Package) {
			p.Messages[0].Payloads = append(p.Messages[0].Payloads, payload.NewString("extra"))
		},
		"invalid result":    func(p *relay.Package) { p.Messages[0].Payloads[1] = payload.NewPayload([]byte(`{}`), payload.JSON) },
		"closed connection": func(p *relay.Package) { ch := make(chan struct{}); close(ch); p.Ingress.ConnectionClosed = ch },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			original := completionFixture(t)
			pkg, err := EncodeCompletion(original)
			require.NoError(t, err)
			defer relay.ReleasePackage(pkg)
			protectControl(pkg)
			mutate(pkg)
			_, err = DecodeCompletion(pkg, original.Monitor.Watcher.Node)
			require.Error(t, err)
		})
	}
}
