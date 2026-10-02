// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"github.com/wippyai/runtime/api/attrs"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
)

// evalPolicy is the only security policy in an eval process's scope, so
// every action it does not allow is denied: eval code has no ambient
// authority beyond its admission policy.
type evalPolicy struct {
	targets map[string]struct{}
	id      registry.ID
	send    apihost.EvalSendMode
	spawn   bool
}

var _ secapi.Policy = (*evalPolicy)(nil)

func newEvalPolicy(name string, policy apihost.EvalPolicy) *evalPolicy {
	p := &evalPolicy{
		id:   registry.ID{NS: EvalProgramNamespace, Name: name},
		send: policy.SendMode,
	}
	for _, command := range policy.AllowCommands {
		if command == apihost.EvalCommandSpawn {
			p.spawn = true
		}
	}
	if policy.SendMode == apihost.EvalSendExplicitGrant {
		p.targets = make(map[string]struct{}, len(policy.SendTargets))
		for _, target := range policy.SendTargets {
			p.targets[target.String()] = struct{}{}
		}
	}
	return p
}

func (p *evalPolicy) ID() registry.ID { return p.id }

func (p *evalPolicy) Evaluate(_ secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	switch action {
	case "process.send":
		switch p.send {
		case apihost.EvalSendObjectCapability:
			// The process's send grants decide which PIDs it may address.
			return secapi.Allow
		case apihost.EvalSendExplicitGrant:
			if _, ok := p.targets[resource]; ok {
				return secapi.Allow
			}
		}
		return secapi.Deny
	case "process.spawn", "process.spawn.linked", "process.spawn.monitored",
		"process.exec", "process.host", "process.context":
		if p.spawn {
			return secapi.Allow
		}
		return secapi.Deny
	}
	return secapi.Undefined
}
