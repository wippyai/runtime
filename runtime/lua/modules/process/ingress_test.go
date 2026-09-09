// SPDX-License-Identifier: MPL-2.0
package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestMessageIngressIsNativeEvidenceWithExactLiveConnection(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	first, replacement := make(chan struct{}), make(chan struct{})
	identity := relay.IngressIdentity{Node: "peer", Authenticated: true, IntegrityProtected: true, ConnectionClosed: first}
	add := func(name string, identity relay.IngressIdentity) {
		msg := NewMessage(pid.PID{Node: "payload-origin"}, "request", nil)
		msg.ingress = identity
		l.SetGlobal(name, WrapMessage(l, msg))
	}
	add("a", identity)
	add("b", identity)
	identity.ConnectionClosed = replacement
	add("replacement", identity)
	identity.ConnectionClosed = nil
	add("no_lifetime", identity)
	add("local_message", relay.IngressIdentity{})
	require.NoError(t, l.DoString(`
  original = a:ingress()
  assert(original:node() == "peer")
  assert(string.find(a:from(), "payload-origin", 1, true))
  assert(original:authenticated() and original:integrity_protected())
  assert(original:live())
  assert(original:same_connection(b:ingress()))
  assert(not original:same_connection(replacement:ingress()))
  assert(not no_lifetime:ingress():live())
  assert(not no_lifetime:ingress():same_connection(no_lifetime:ingress()))
  assert(local_message:ingress() == nil)
  assert(not pcall(function() original:same_connection({node="peer"}) end))
  assert(not pcall(function() original.node = "forged" end))
 `))
	close(first)
	require.NoError(t, l.DoString(`
  assert(not original:live())
  assert(not a:ingress():live())
  assert(replacement:ingress():live())
  assert(original:same_connection(b:ingress())) -- equality does not renew it
 `))
}
