// SPDX-License-Identifier: MPL-2.0

package process

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const ingressTypeName = "process.Ingress"

// ingressHandle is immutable transport evidence, not a grant or origin claim.
// The exact connection lifetime stays native and cannot be minted from Lua data.
type ingressHandle struct{ identity relay.IngressIdentity }

func init() {
	value.RegisterTypeMethods(nil, ingressTypeName, nil, map[string]lua.LGoFunc{
		"node":                ingressNode,
		"authenticated":       ingressAuthenticated,
		"integrity_protected": ingressIntegrityProtected,
		"live":                ingressLive,
		"same_connection":     ingressSameConnection,
	})
}

func messageIngress(l *lua.LState) int {
	msg := checkMessage(l)
	if msg == nil {
		return 0
	}
	if msg.ingress == (relay.IngressIdentity{}) {
		l.Push(lua.LNil)
	} else {
		l.Push(value.NewTypedUserData(l, &ingressHandle{identity: msg.ingress}, ingressTypeName))
	}
	return 1
}

func checkIngress(l *lua.LState, index int) *ingressHandle {
	ud := l.CheckUserData(index)
	if handle, ok := ud.Value.(*ingressHandle); ok {
		return handle
	}
	l.ArgError(index, "native ingress expected")
	return nil
}

func ingressNode(l *lua.LState) int {
	h := checkIngress(l, 1)
	if h == nil {
		return 0
	}
	l.Push(lua.LString(h.identity.Node))
	return 1
}
func ingressAuthenticated(l *lua.LState) int {
	h := checkIngress(l, 1)
	if h == nil {
		return 0
	}
	l.Push(lua.LBool(h.identity.Authenticated))
	return 1
}
func ingressIntegrityProtected(l *lua.LState) int {
	h := checkIngress(l, 1)
	if h == nil {
		return 0
	}
	l.Push(lua.LBool(h.identity.IntegrityProtected))
	return 1
}
func ingressLive(l *lua.LState) int {
	h := checkIngress(l, 1)
	if h == nil {
		return 0
	}
	live := h.identity.ConnectionClosed != nil
	if live {
		select {
		case <-h.identity.ConnectionClosed:
			live = false
		default:
		}
	}
	l.Push(lua.LBool(live))
	return 1
}
func ingressSameConnection(l *lua.LState) int {
	h := checkIngress(l, 1)
	if h == nil {
		return 0
	}
	other := checkIngress(l, 2)
	if other == nil {
		return 0
	}
	// This is identity equality only. Admission must also check live() at use.
	same := h.identity.ConnectionClosed != nil && h.identity == other.identity
	l.Push(lua.LBool(same))
	return 1
}
