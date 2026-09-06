// SPDX-License-Identifier: MPL-2.0

// tty-mesh proves the real Lua -> actor -> authenticated internode -> viewport
// -> Lua -> native PTY -> VT -> snapshot path between two independent nodes.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaexec "github.com/wippyai/runtime/runtime/lua/modules/exec"
	luatty "github.com/wippyai/runtime/runtime/lua/modules/tty"
	execdispatcher "github.com/wippyai/runtime/service/exec"
	"github.com/wippyai/runtime/service/exec/native"
	ttydispatcher "github.com/wippyai/runtime/service/terminal/tty"
	relaysys "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	securitysys "github.com/wippyai/runtime/system/security"
	ttysys "github.com/wippyai/runtime/system/tty"
	"go.uber.org/zap"
)

type proofKeys struct {
	A, B   []byte
	Secret []byte
}
type references struct{ Observe, Input string }
type transport struct{ cm internode.ConnectionManager }

func (t transport) Send(peer string, b []byte) error {
	return t.cm.SendToNode(peer, b, internode.ClassSurface)
}
func (t transport) Receive(fn func(string, []byte)) error {
	if !t.cm.RegisterClassReceiver(internode.ClassSurface, fn) {
		return errors.New("receiver already installed")
	}
	return nil
}

// The proof registers one real native executor without booting a registry DB.
// All Lua security checks and the normal exec/PTY implementation still run.
type executorRegistry struct{ executor execapi.ProcessExecutor }

func (r executorRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	if id.String() != "proof:exec" {
		return nil, errors.New("unknown executor")
	}
	return &executorResource{value: r.executor}, nil
}
func (executorRegistry) List() ([]registry.ID, error) {
	return []registry.ID{registry.ParseID("proof:exec")}, nil
}
func (executorRegistry) Exists(id registry.ID) bool { return id.String() == "proof:exec" }

type executorResource struct{ value any }

func (r *executorResource) Get() (any, error) { return r.value, nil }
func (*executorResource) Release()            {}

type proofPolicy struct{}

func (proofPolicy) ID() registry.ID { return registry.ParseID("proof:policy") }
func (proofPolicy) Evaluate(_ security.Actor, action, _ string, _ attrs.Bag) security.Result {
	switch action {
	case "tty.mount", ttyapi.RightObserve, ttyapi.RightInput, ttyapi.RightResize, "exec.get", "exec.run":
		return security.Allow
	}
	return security.Deny
}

type completion struct {
	result *runtime.Result
	pid    pid.PID
}
type lifecycle struct {
	service   *ttysys.Service
	completed chan completion
}

func (*lifecycle) OnStart(context.Context, pid.PID, processapi.Process) error { return nil }
func (l *lifecycle) OnComplete(ctx context.Context, p pid.PID, r *runtime.Result) {
	l.service.OnComplete(ctx, p, r)
	l.completed <- completion{pid: p, result: r}
}

type runner struct {
	root      context.Context
	scheduler *actor.Scheduler
	service   *ttysys.Service
	completed chan completion
	local     string
	peer      string
	out       string
	frames    []ctxapi.FrameContext
	latencies []time.Duration
	mu        sync.Mutex
}

func (r *runner) spawn(id, script, grant string, refs references) error {
	ctx, frame := ctxapi.OpenFrameContext(r.root)
	p := pid.PID{Node: r.local, Host: "agents", UniqID: id}
	if err := frame.Set(runtime.FramePIDKey, p); err != nil {
		return err
	}
	if err := security.SetActor(ctx, security.Actor{ID: "proof:" + id}); err != nil {
		return err
	}
	if err := security.SetScope(ctx, securitysys.NewScope([]security.Policy{proofPolicy{}})); err != nil {
		return err
	}
	if grant != "" {
		options := attrs.NewBagFrom(map[string]any{ttyapi.OptionTerminal: grant})
		pairs, err := ttyapi.ResolveTerminalOption(ctx, options)
		if err != nil {
			return err
		}
		if err := frame.SetMultiple(pairs...); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.frames = append(r.frames, frame)
	r.mu.Unlock()
	proc, err := engine.NewProcess(engine.WithScript("local raw_assert = assert; local function assert(ok, err) return raw_assert(ok, tostring(err)) end\n"+script, id+".lua"), engine.WithModuleBinder(func(l *lua.LState) error {
		engine.BindCachedLibs(l)
		engine.LoadModuleDef(l, engine.ChannelModule)
		engine.LoadModuleDef(l, luatty.Module)
		engine.LoadModuleDef(l, luaexec.Module)
		l.SetGlobal("recipient", lua.LString((&pid.PID{Node: r.peer, Host: "agents", UniqID: "agent"}).String()))
		l.SetGlobal("observe_ref", lua.LString(refs.Observe))
		l.SetGlobal("input_ref", lua.LString(refs.Input))
		l.SetGlobal("spawn_child", lua.LGoFunc(func(l *lua.LState) int {
			if err := r.spawn("child", childScript, l.CheckString(1), references{}); err != nil {
				l.RaiseError("spawn: %v", err)
			}
			return 0
		}))
		l.SetGlobal("publish", lua.LGoFunc(func(l *lua.LState) int {
			b, err := json.Marshal(references{Observe: l.CheckString(1), Input: l.CheckString(2)})
			if err == nil {
				err = os.WriteFile(r.out, b, 0600)
			}
			if err != nil {
				l.RaiseError("publish: %v", err)
			}
			return 0
		}))
		var start time.Time
		l.SetGlobal("measure", lua.LGoFunc(func(l *lua.LState) int {
			if l.CheckString(1) == "start" {
				start = time.Now()
			} else {
				r.mu.Lock()
				r.latencies = append(r.latencies, time.Since(start))
				r.mu.Unlock()
			}
			return 0
		}))
		return nil
	}))
	if err != nil {
		return err
	}
	if _, err = r.scheduler.Submit(ctx, p, proc, "", nil); err != nil {
		proc.Close()
		return err
	}
	return nil
}

const ownerScript = `
local view = assert(tty.viewport({width=80,height=24}))
local observe = assert(view:mount(recipient,{observe=true}))
local input = assert(view:mount(recipient,{input=true,resize=true}))
spawn_child(assert(view:grant()))
publish(observe,input)
channel.new():receive()
`

// The child is unchanged by transport placement. It receives only its normal
// terminal binding, just like tests/tty-surface-demo/src/child.lua.
const childScript = `
local events = assert(tty.events())
assert(tty.start())
local executor = assert(exec.get("proof:exec"))
local child = assert(executor:exec("/bin/bash --noprofile --norc", {pty={term="xterm-256color"},env={PS1="MESH_READY> "}}))
local session = assert(child:attach_terminal())
local done = session:done()
while true do
 local selected=channel.select({events:case_receive(),done:case_receive()})
 if not selected.ok or selected.channel==done then break end
 if selected.value.type=="close" then break end
 assert(session:send(selected.value))
end
assert(session:close())
assert(executor:release())
`
const agentScript = `
local observer=assert(tty.attach(observe_ref))
local control=assert(tty.attach(input_ref))
local denied,err=observer:send({type="key",key="x",key_type="runes",action="press"})
assert(denied==nil and err,"observe mount gained input")
local hidden,read_err=control:snapshot()
assert(hidden==nil and read_err,"input mount leaked snapshot")
local stream,stream_err=control:updates()
assert(stream==nil and stream_err,"input mount leaked updates")
local updates=assert(observer:updates())
local function wait_for(marker)
 while true do
  local snapshot=assert(observer:snapshot())
  if string.find(table.concat(snapshot.rows,"\n"),marker,1,true) then return snapshot end
  local _,open=updates:receive()
  assert(open,"observer closed before expected screen")
 end
end
wait_for("MESH_READY>")
assert(control:resize(100,30))
for i=1,20 do
 measure("start")
 -- Split the marker in the echoed command so only executed Bash output can
 -- satisfy the screen assertion. Clear the screen to test actual VT state.
 local cmd="printf '\\033[2J\\033[H%s\\n' 'MESH_'\"PROOF_"..i.."\""
 assert(control:send({type="paste",text=cmd}))
 assert(control:send({type="key",key="enter",key_type="enter",action="press"}))
 local snapshot=wait_for("MESH_PROOF_"..i)
 assert(snapshot.width==100 and snapshot.height==30)
 measure("end")
end
assert(control:close())
assert(observer:close())
return "mesh Lua PTY proof passed"
`

func initKeys(dir, ips string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_, a, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	_, b, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	data, err := json.Marshal(proofKeys{A: a, B: b, Secret: secret})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "keys.json"), data, 0600); err != nil {
		return err
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "tty-mesh-proof"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	for _, s := range strings.Split(ips, ",") {
		ip := net.ParseIP(s)
		if ip == nil {
			return fmt.Errorf("invalid IP %q", s)
		}
		cert.IPAddresses = append(cert.IPAddresses, ip)
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return err
	}
	der, err = x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	initDir := flag.String("init", "", "generate disposable proof identities and TLS certificate")
	ips := flag.String("ips", "127.0.0.1", "comma-separated certificate IPs")
	keysDir := flag.String("keys", "", "proof identity directory")
	local := flag.String("node", "a", "a or b")
	bind := flag.String("bind", "127.0.0.1", "listen address")
	port := flag.Int("port", 19470, "internode listen port")
	peerAddr := flag.String("peer-address", "127.0.0.1", "peer address")
	peerPort := flag.Int("peer-port", 19471, "peer port")
	out := flag.String("refs-out", "", "local references file")
	peerFile := flag.String("peer-refs", "", "peer references file")
	hold := flag.Duration("hold", 3*time.Second, "keep local producer alive after agent completes")
	flag.Parse()
	if *initDir != "" {
		return initKeys(*initDir, *ips)
	}
	if *keysDir == "" || *out == "" || *peerFile == "" || (*local != "a" && *local != "b") {
		return errors.New("keys, refs-out, peer-refs and node a/b are required")
	}
	data, err := os.ReadFile(filepath.Join(*keysDir, "keys.json"))
	if err != nil {
		return err
	}
	var keys proofKeys
	if err = json.Unmarshal(data, &keys); err != nil {
		return err
	}
	peer := "b"
	own, other := keys.A, keys.B
	if *local == "b" {
		peer = "a"
		own, other = keys.B, keys.A
	}
	if len(own) != ed25519.PrivateKeySize || len(other) != ed25519.PrivateKeySize {
		return errors.New("invalid proof key")
	}
	cfg := internode.DefaultManagerConfig()
	cfg.LocalNodeID = *local
	cfg.BindAddr = *bind
	cfg.BindPort = *port
	cfg.AutoPort = false
	cfg.Logger = zap.NewNop()
	cfg.AuthenticationKey = keys.Secret
	cfg.SigningKey = ed25519.PrivateKey(own)
	cfg.ResolvePeerKey = func(id string) (ed25519.PublicKey, bool) {
		return ed25519.PrivateKey(other).Public().(ed25519.PublicKey), id == peer
	}
	cfg.AuthorizePeer = func(id string, _ net.Addr) bool { return id == peer }
	cfg.TLS = internode.ManagerTLSConfig{Enabled: true, CertFile: filepath.Join(*keysDir, "cert.pem"), CAFile: filepath.Join(*keysDir, "cert.pem"), KeyFile: filepath.Join(*keysDir, "key.pem")}
	cm := internode.NewConnectionManager(cfg, nil)
	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 90*time.Second)
	defer cancel()
	if err = cm.Start(ctx, func(string, []byte) {}); err != nil {
		return err
	}
	defer func() { _ = cm.Stop() }()
	cm.AddManagedNode(peer)
	cm.EnsureConnection(peer, *peerAddr, *peerPort)
	service := ttysys.NewService()
	defer service.Close()
	if err = service.SetMesh(*local, transport{cm}); err != nil {
		return err
	}
	root := ttyapi.WithService(ctx, service)
	root = resource.WithRegistry(root, executorRegistry{native.NewNativeExecutor(zap.NewNop(), &execapi.NativeExecutorConfig{})})
	dispatch := ttydispatcher.NewDispatcher()
	if err = dispatch.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = dispatch.Stop(context.Background()) }()
	reg := scheduler.NewRegistry()
	dispatch.RegisterAll(reg.Register)
	execdispatcher.NewDispatcher().RegisterAll(reg.Register)
	completed := make(chan completion, 16)
	sched := actor.NewScheduler(reg, actor.WithWorkers(1), actor.WithLifecycle(&lifecycle{service, completed}))
	node := relaysys.NewNode(*local)
	if err = node.RegisterHost("agents", sched); err != nil {
		return err
	}
	root = relay.WithNode(root, node)
	sched.Start()

	r := &runner{root: root, scheduler: sched, service: service, local: *local, peer: peer, out: *out, completed: completed}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		sched.Stop(stop)
		for _, f := range r.frames {
			_ = f.Close()
		}
	}()
	if err = r.spawn("owner", ownerScript, "", references{}); err != nil {
		return err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var refs references
waitPeer:
	for {
		select {
		case c := <-completed:
			if c.result != nil && c.result.Error != nil {
				return c.result.Error
			}
			return fmt.Errorf("%s ended before peer attached", c.pid.UniqID)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			data, err = os.ReadFile(*peerFile)
			if err == nil && json.Unmarshal(data, &refs) == nil && refs.Observe != "" && len(cm.ConnectedNodes()) > 0 {
				break waitPeer
			}
		}
	}
	if err = r.spawn("agent", agentScript, "", refs); err != nil {
		return err
	}
	for {
		select {
		case c := <-completed:
			if c.result != nil && c.result.Error != nil {
				return fmt.Errorf("%s: %w", c.pid.UniqID, c.result.Error)
			}
			if c.pid.UniqID != "agent" {
				return fmt.Errorf("unexpected process exit: %s", c.pid.UniqID)
			}
			r.mu.Lock()
			samples := append([]time.Duration(nil), r.latencies...)
			r.mu.Unlock()
			if len(samples) != 20 {
				return fmt.Errorf("expected 20 command results, got %d", len(samples))
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			report := map[string]any{"node": *local, "peer": peer, "commands": len(samples), "p50_ms": float64(samples[len(samples)/2]) / float64(time.Millisecond), "p95_ms": float64(samples[len(samples)*95/100]) / float64(time.Millisecond), "result": "PASS", "transport": "mutual TLS + authenticated internode", "lua_workers": 1}
			encoded, _ := json.Marshal(report)
			fmt.Println(string(encoded))
			select {
			case <-time.After(*hold):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
