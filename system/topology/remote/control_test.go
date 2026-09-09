// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/internode"
)

func testControl() Control {
	return Control{Kind: MonitorControl, RequestID: strings.Repeat("a", 32), Grant: strings.Repeat("b", 64),
		Watcher: pid.PID{Node: "watcher-node", Host: "workers", UniqID: "watcher"},
		Target:  pid.PID{Node: "target-node", Host: "workers", UniqID: "target"}}
}

func protectControl(pkg *relay.Package) {
	pkg.Ingress = relay.IngressIdentity{Node: pkg.Source.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: make(chan struct{})}
}

func TestControlNativeCodecRoundTrip(t *testing.T) {
	for _, kind := range []ControlKind{MonitorControl, ReleaseControl, InstalledControl, ReleasedControl, RejectedControl, MissingControl} {
		t.Run(string(kind), func(t *testing.T) {
			control := testControl()
			control.Kind = kind
			pkg, err := EncodeControl(control)
			require.NoError(t, err)
			codec := internode.NewMessageCodec(nil)
			encoded, err := codec.Encode(pkg)
			relay.ReleasePackage(pkg)
			require.NoError(t, err)
			decoded, err := codec.Decode(encoded)
			require.NoError(t, err)
			defer relay.ReleasePackage(decoded)
			// The service installs ingress from the connection, never the wire.
			protectControl(decoded)
			result, err := DecodeControl(decoded, decoded.Target.Node)
			require.NoError(t, err)
			require.Equal(t, kind, result.Kind)
			require.Equal(t, control.Watcher.String(), result.Watcher.String())
			require.Equal(t, control.Target.String(), result.Target.String())
			require.Equal(t, control.RequestID, result.RequestID)
		})
	}
}

func TestControlRejectsUntrustedEnvelopes(t *testing.T) {
	cases := map[string]func(*relay.Package){
		"unauthenticated":       func(p *relay.Package) { p.Ingress.Authenticated = false },
		"plaintext":             func(p *relay.Package) { p.Ingress.IntegrityProtected = false },
		"no lifetime":           func(p *relay.Package) { p.Ingress.ConnectionClosed = nil },
		"closed connection":     func(p *relay.Package) { ch := make(chan struct{}); close(ch); p.Ingress.ConnectionClosed = ch },
		"different peer":        func(p *relay.Package) { p.Ingress.Node = "other" },
		"different actor":       func(p *relay.Package) { p.Source.UniqID = "other" },
		"different source host": func(p *relay.Package) { p.Source.Host = "other" },
		"different destination": func(p *relay.Package) { p.Target.Node = "other" },
		"actor destination":     func(p *relay.Package) { p.Target.UniqID = "actor" },
		"ordinary host":         func(p *relay.Package) { p.Target.Host = "workers" },
		"mixed package":         func(p *relay.Package) { p.Messages = append(p.Messages, relay.AcquireMessage()) },
		"extra payload":         func(p *relay.Package) { p.Messages[0].Payloads = append(p.Messages[0].Payloads, payload.New("extra")) },
		"wrong topic":           func(p *relay.Package) { p.Messages[0].Topic = "@pid/events" },
		"wrong format":          func(p *relay.Package) { p.Messages[0].Payloads[0] = payload.NewPayload([]byte(`{}`), payload.Bytes) },
		"oversized": func(p *relay.Package) {
			p.Messages[0].Payloads[0] = payload.NewPayload(make([]byte, MaxControlBytes+1), payload.JSON)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			pkg, err := EncodeControl(testControl())
			require.NoError(t, err)
			defer relay.ReleasePackage(pkg)
			protectControl(pkg)
			mutate(pkg)
			_, err = DecodeControl(pkg, "target-node")
			require.ErrorIs(t, err, ErrInvalidControl)
		})
	}
}

func TestControlRejectsAmbiguousJSON(t *testing.T) {
	pkg, err := EncodeControl(testControl())
	require.NoError(t, err)
	defer relay.ReleasePackage(pkg)
	original := string(pkg.Messages[0].Payloads[0].Data().([]byte))
	for _, data := range []string{
		original + ` {}`, strings.Replace(original, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(original, `"version":1`, `"version":2`, 1),
		strings.Replace(original, `"version":1`, `"unknown":1`, 1),
		strings.Replace(original, `"version":1`, `"version":null`, 1),
		strings.Replace(original, `"kind":"monitor"`, `"kind":"link"`, 1),
		strings.Replace(original, `workers|watcher`, `workers|ignored|watcher`, 1),
		strings.Replace(original, `"kind":"monitor"`, `"kind":null`, 1),
		`[]`, `null`, string([]byte{0xff}),
	} {
		protectControl(pkg)
		pkg.Messages[0].Payloads[0] = payload.NewPayload([]byte(data), payload.JSON)
		_, err := DecodeControl(pkg, "target-node")
		require.ErrorIs(t, err, ErrInvalidControl)
	}
}

func FuzzDecodeControl(f *testing.F) {
	pkg, err := EncodeControl(testControl())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(pkg.Messages[0].Payloads[0].Data().([]byte))
	relay.ReleasePackage(pkg)
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := EncodeControl(testControl())
		require.NoError(t, err)
		defer relay.ReleasePackage(pkg)
		protectControl(pkg)
		pkg.Messages[0].Payloads[0] = payload.NewPayload(data, payload.JSON)
		_, _ = DecodeControl(pkg, "target-node")
	})
}
