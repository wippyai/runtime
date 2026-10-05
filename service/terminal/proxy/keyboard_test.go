// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestKittyKeyboardNegotiationThroughEmulator(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, "\x1b[>3u\x1b[?u")
	go func() { _ = proxy.copyResponsesOnce() }()
	require.Equal(t, "\x1b[?3u", string(<-process.input))

	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "down", Action: "press"}))
	require.Equal(t, "\x1b[B", string(<-process.input))
	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "down", Action: "release"}))
	require.Equal(t, "\x1b[1;1:3B", string(<-process.input))

	proxy.feed(t, "\x1b[<u\x1b[?u")
	go func() { _ = proxy.copyResponsesOnce() }()
	require.Equal(t, "\x1b[?0u", string(<-process.input))
}

func TestModifyOtherKeysNegotiationThroughEmulator(t *testing.T) {
	proxy, process := newModeProxy(t)
	proxy.feed(t, "\x1b[>4;2m")
	require.NoError(t, proxy.handle(ttyapi.Event{Type: "key", KeyType: "runes", Key: "x", Ctrl: true, Action: "press"}))
	require.Equal(t, "\x1b[27;5;120~", string(<-process.input))
}

func (p *Proxy) copyResponsesOnce() error {
	data, ok := p.responses.next()
	if !ok {
		return nil
	}
	return p.writeBytes(data)
}
