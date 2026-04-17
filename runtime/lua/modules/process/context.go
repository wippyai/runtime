// SPDX-License-Identifier: MPL-2.0

package process

import (
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
)

const spawnerTypeName = "process.Spawner"

// Spawner represents a process spawner with context values
type Spawner struct {
	actor    secapi.Actor
	scope    secapi.Scope
	values   ctxapi.Values
	options  attrs.Bag
	name     string
	messages []*relay.Message
	hasActor bool
	hasScope bool
	hasOpts  bool
}

func init() {
	value.RegisterTypeMethods(nil, spawnerTypeName, nil, map[string]lua.LGoFunc{
		"with_context":           spawnerWithContext,
		"with_options":           spawnerWithOptions,
		"with_actor":             spawnerWithActor,
		"with_scope":             spawnerWithScope,
		"with_name":              spawnerWithName,
		"with_message":           spawnerWithMessage,
		"spawn":                  spawnerSpawn,
		"spawn_monitored":        spawnerSpawnMonitored,
		"spawn_linked":           spawnerSpawnLinked,
		"spawn_linked_monitored": spawnerSpawnLinkedMonitored,
	})
}

func cloneSpawner(spawner *Spawner) *Spawner {
	if spawner == nil {
		return &Spawner{}
	}

	return &Spawner{
		values:   spawner.values,
		options:  spawner.options,
		actor:    spawner.actor,
		hasActor: spawner.hasActor,
		scope:    spawner.scope,
		hasScope: spawner.hasScope,
		hasOpts:  spawner.hasOpts,
		name:     spawner.name,
		messages: spawner.messages,
	}
}

func parseSpawnerOptions(l *lua.LState, idx int) attrs.Bag {
	optionsTable := l.CheckTable(idx)
	options := attrs.NewBag()

	optionsTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(idx, "options keys must be strings")
			return
		}
		options.Set(string(key), value.ToGoAny(v))
	})

	if net := options.GetString(netapi.OptionKeyNetwork, ""); net != "" {
		if !security.IsAllowed(l.Context(), "network.select", net, nil) {
			l.RaiseError("not allowed: network %s", net)
			return nil
		}
	}

	return options
}

// spawnerNew creates a new process spawner (process.with_context)
func spawnerNew(l *lua.LState) int {
	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.context", "context", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom context")
		return 0
	}

	values := ctxapi.GetValues(ctx)
	if values != nil {
		values = values.Clone().(ctxapi.Values)
	} else {
		values = ctxapi.NewValues()
	}

	ctxTable := l.CheckTable(1)
	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(1, "context keys must be strings")
			return
		}
		values.Set(string(key), value.ToGoAny(v))
	})

	var actor secapi.Actor
	var scope secapi.Scope
	hasActor := false
	hasScope := false

	if a, ok := secapi.GetActor(ctx); ok {
		actor = a
		hasActor = true
	}
	if s, ok := secapi.GetScope(ctx); ok {
		scope = s
		hasScope = true
	}

	spawner := &Spawner{
		values:   values,
		actor:    actor,
		hasActor: hasActor,
		scope:    scope,
		hasScope: hasScope,
	}

	value.PushTypedUserData(l, spawner, spawnerTypeName)
	return 1
}

// spawnerNewWithOptions creates a new process spawner (process.with_options)
func spawnerNewWithOptions(l *lua.LState) int {
	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.context", "context", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom options")
		return 0
	}

	values := ctxapi.GetValues(ctx)
	if values != nil {
		values = values.Clone().(ctxapi.Values)
	} else {
		values = ctxapi.NewValues()
	}

	var actor secapi.Actor
	var scope secapi.Scope
	hasActor := false
	hasScope := false

	if a, ok := secapi.GetActor(ctx); ok {
		actor = a
		hasActor = true
	}
	if s, ok := secapi.GetScope(ctx); ok {
		scope = s
		hasScope = true
	}

	spawner := &Spawner{
		values:   values,
		options:  parseSpawnerOptions(l, 1),
		actor:    actor,
		hasActor: hasActor,
		scope:    scope,
		hasScope: hasScope,
		hasOpts:  true,
	}

	value.PushTypedUserData(l, spawner, spawnerTypeName)
	return 1
}

// spawnerWithName sets the process name for spawn-or-signal
func spawnerWithName(l *lua.LState) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	name := l.CheckString(2)
	if name == "" {
		l.ArgError(2, "name cannot be empty")
		return 0
	}

	newSpawner := cloneSpawner(spawner)
	newSpawner.name = name

	value.PushTypedUserData(l, newSpawner, spawnerTypeName)
	return 1
}

// spawnerWithMessage adds a message to be sent after spawn (or to existing process if name taken)
func spawnerWithMessage(l *lua.LState) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	topic := l.CheckString(2)
	if strings.HasPrefix(topic, "@") {
		l.ArgError(2, "cannot send to @ topics")
		return 0
	}

	var payloads payload.Payloads
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	msg := relay.AcquireMessage()
	msg.Topic = topic
	msg.Payloads = payloads

	newMessages := make([]*relay.Message, len(spawner.messages)+1)
	copy(newMessages, spawner.messages)
	newMessages[len(spawner.messages)] = msg

	newSpawner := cloneSpawner(spawner)
	newSpawner.messages = newMessages

	value.PushTypedUserData(l, newSpawner, spawnerTypeName)
	return 1
}

// spawnerWithContext adds context values to the spawner
func spawnerWithContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.context", "context", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom context")
		return 0
	}

	ctxTable := l.CheckTable(2)

	if (spawner.hasScope || spawner.hasActor) && !security.IsAllowed(ctx, "process.security", "security", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom security context")
		return 0
	}

	newValues := ctxapi.NewValues()
	if spawner.values != nil {
		spawner.values.Iterate(func(key string, val any) {
			newValues.Set(key, val)
		})
	}

	ctxTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			newValues.Set(string(key), value.ToGoAny(v))
		}
	})

	newSpawner := &Spawner{
		values:   newValues,
		options:  spawner.options,
		actor:    spawner.actor,
		hasActor: spawner.hasActor,
		scope:    spawner.scope,
		hasScope: spawner.hasScope,
		hasOpts:  spawner.hasOpts,
		name:     spawner.name,
		messages: spawner.messages,
	}

	value.PushTypedUserData(l, newSpawner, spawnerTypeName)
	return 1
}

// spawnerWithOptions adds spawn options to the spawner
func spawnerWithOptions(l *lua.LState) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.context", "context", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom options")
		return 0
	}

	newOptions := parseSpawnerOptions(l, 2)

	existing := spawner.options
	if existing == nil {
		existing = attrs.NewBag()
	}

	newSpawner := cloneSpawner(spawner)
	newSpawner.options = existing.Merge(newOptions)
	newSpawner.hasOpts = true

	value.PushTypedUserData(l, newSpawner, spawnerTypeName)
	return 1
}

// spawnerWithActor sets the actor on the spawner
func spawnerWithActor(l *lua.LState) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.security", "security", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom security context")
		return 0
	}

	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "actor cannot be nil")
		return 0
	}

	actorUD := l.CheckUserData(2)
	actor, ok := actorUD.Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	newSpawner := cloneSpawner(spawner)
	newSpawner.actor = actor
	newSpawner.hasActor = true

	value.PushTypedUserData(l, newSpawner, spawnerTypeName)
	return 1
}

// spawnerWithScope sets the scope on the spawner
func spawnerWithScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.security", "security", secAttrs) {
		l.RaiseError("not allowed to spawn processes with custom security context")
		return 0
	}

	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "scope cannot be nil")
		return 0
	}

	scopeUD := l.CheckUserData(2)
	scope, ok := scopeUD.Value.(secapi.Scope)
	if !ok {
		l.ArgError(2, "Scope expected")
		return 0
	}

	newSpawner := cloneSpawner(spawner)
	newSpawner.scope = scope
	newSpawner.hasScope = true

	value.PushTypedUserData(l, newSpawner, spawnerTypeName)
	return 1
}

// buildSpawnerContext builds context.Pair slice from Spawner
func buildSpawnerContext(spawner *Spawner) []ctxapi.Pair {
	if spawner == nil {
		return nil
	}

	var pairs []ctxapi.Pair

	if spawner.hasActor {
		pairs = append(pairs, secapi.ActorPair(spawner.actor))
	}

	if spawner.hasScope {
		pairs = append(pairs, secapi.ScopePair(spawner.scope))
	}

	if spawner.values != nil && spawner.values.Len() > 0 {
		pairs = append(pairs, ctxapi.ValuesPair(spawner.values))
	}

	return pairs
}

// doSpawnerSpawn is the common implementation for all spawner spawn variants
func doSpawnerSpawn(l *lua.LState, monitored, linked bool) int {
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*Spawner)
	if !ok {
		l.ArgError(1, "Spawner expected")
		return 0
	}

	if l.GetTop() < 3 {
		l.RaiseError("spawn requires at least id and host arguments")
		return 0
	}

	id := l.CheckString(2)
	hostID := l.CheckString(3)

	ctx := l.Context()

	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(ctx, "process.spawn", id, secAttrs) {
		l.RaiseError("not allowed to spawn process: %s", id)
		return 0
	}

	if monitored && !security.IsAllowed(ctx, "process.spawn.monitored", id, secAttrs) {
		l.RaiseError("not allowed to spawn monitored process: %s", id)
		return 0
	}

	if linked && !security.IsAllowed(ctx, "process.spawn.linked", id, secAttrs) {
		l.RaiseError("not allowed to spawn linked process: %s", id)
		return 0
	}

	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	options := attrs.NewBag()
	if spawner.hasOpts && spawner.options != nil {
		options = spawner.options.Clone().(attrs.Bag)
	}
	options.Set(process.ProcessParentKey, self)
	if spawner.name != "" {
		options.Set(process.ProcessNameKey, spawner.name)
	}
	if monitored {
		options.Set(process.ProcessMonitorKey, true)
	}
	if linked {
		options.Set(process.ProcessLinkKey, true)
	}

	yield := AcquireSpawnYield()
	yield.Start = &process.Start{
		HostID:   hostID,
		Source:   registry.ParseID(id),
		Input:    payloads,
		Context:  buildSpawnerContext(spawner),
		Options:  options,
		Messages: spawner.messages,
	}

	l.Push(yield)
	return -1
}

func spawnerSpawn(l *lua.LState) int                { return doSpawnerSpawn(l, false, false) }
func spawnerSpawnMonitored(l *lua.LState) int       { return doSpawnerSpawn(l, true, false) }
func spawnerSpawnLinked(l *lua.LState) int          { return doSpawnerSpawn(l, false, true) }
func spawnerSpawnLinkedMonitored(l *lua.LState) int { return doSpawnerSpawn(l, true, true) }
