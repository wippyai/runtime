// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	localrelay "github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

const participantProcessEnv = "WIPPY_PARTICIPANT_PROCESS_CONFIG"

type participantProcessConfig struct {
	Node, Peer   string
	TLS          internode.ManagerTLSConfig
	Signing      ed25519.PrivateKey
	PeerPublic   ed25519.PublicKey
	LoadDuration time.Duration
	Claims       int
}
type participantProcessMessage struct {
	Op     string
	Port   int
	Report *participantLoadReport `json:",omitempty"`
}

type participantLoadReport struct {
	Completed, Busy                   int
	Elapsed                           time.Duration
	Allocated, HeapBefore, HeapAfter  uint64
	GoroutinesBefore, GoroutinesAfter int
}

// The parent controls scheduling only. Each child owns separate Go memory, KV,
// relay, TLS listener, admission and endpoint lifetimes. Naming bytes cross TCP.
func TestParticipantIndependentTLSProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("independent process TLS integration")
	}
	var loadDuration time.Duration
	claims := 0
	if raw := os.Getenv("WIPPY_PARTICIPANT_LOAD_DURATION"); raw != "" {
		var err error
		loadDuration, err = time.ParseDuration(raw)
		require.NoError(t, err)
		require.Positive(t, loadDuration)
		claims = 1000
		if raw := os.Getenv("WIPPY_PARTICIPANT_LOAD_CLAIMS"); raw != "" {
			claims, err = strconv.Atoi(raw)
			require.NoError(t, err)
		}
		require.GreaterOrEqual(t, claims, 0)
		require.LessOrEqual(t, claims, 10000)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second+loadDuration)
	defer cancel()
	authorityTLS, clientTLS := participantNativeTLSConfigs(t)
	authorityPublic, authorityPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	clientPublic, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	type child struct {
		cmd    *exec.Cmd
		input  io.WriteCloser
		output *bufio.Reader
		stderr bytes.Buffer
		joined bool
	}
	start := func(cfg participantProcessConfig) *child {
		t.Helper()
		body, err := json.Marshal(cfg)
		require.NoError(t, err)
		configPath := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(configPath, body, 0600))
		c := &child{cmd: exec.CommandContext(ctx, os.Args[0], "-test.run=^TestParticipantProcessHelper$")}
		c.cmd.Env = append(os.Environ(), participantProcessEnv+"="+configPath)
		c.cmd.Stderr = &c.stderr
		stdout, err := c.cmd.StdoutPipe()
		require.NoError(t, err)
		c.output = bufio.NewReader(stdout)
		c.input, err = c.cmd.StdinPipe()
		require.NoError(t, err)
		require.NoError(t, c.cmd.Start())
		t.Cleanup(func() {
			_ = c.input.Close()
			if !c.joined {
				_ = c.cmd.Process.Kill()
				_ = c.cmd.Wait()
			}
		})
		return c
	}
	receive := func(c *child, op string) participantProcessMessage {
		t.Helper()
		line, err := c.output.ReadBytes('\n')
		require.NoError(t, err, "waiting for %s; child output %s", op, line)
		var message participantProcessMessage
		if err := json.Unmarshal(line, &message); err != nil {
			tail, _ := io.ReadAll(c.output)
			t.Fatalf("waiting for %s: %v; child output: %s%s", op, err, line, tail)
		}
		require.Equal(t, op, message.Op)
		return message
	}
	send := func(c *child, op string, port int) {
		t.Helper()
		require.NoError(t, json.NewEncoder(c.input).Encode(participantProcessMessage{Op: op, Port: port}))
	}
	authority := start(participantProcessConfig{"authority", "client", authorityTLS, authorityPrivate, clientPublic, loadDuration, claims})
	client := start(participantProcessConfig{"client", "authority", clientTLS, clientPrivate, authorityPublic, loadDuration, claims})
	authorityPort := receive(authority, "listening").Port
	clientPort := receive(client, "listening").Port
	fragment, delay := 512, time.Millisecond
	if loadDuration > 0 {
		fragment, delay = 16384, 0
	}
	authorityProxy := newParticipantTCPProxy(t, ctx, fmt.Sprintf("127.0.0.1:%d", authorityPort), fragment, delay)
	clientProxy := newParticipantTCPProxy(t, ctx, fmt.Sprintf("127.0.0.1:%d", clientPort), fragment, delay)
	send(authority, "connect", clientProxy.port())
	receive(authority, "ready")
	send(client, "connect", authorityProxy.port())
	receive(client, "ready")
	if loadDuration > 0 {
		send(client, "load", 0)
		report := receive(client, "loaded").Report
		require.NotNil(t, report)
		require.Positive(t, report.Completed)
		t.Logf("native TLS load: claims=%d completed=%d busy=%d elapsed=%s allocated=%d heap=%d->%d goroutines=%d->%d", claims, report.Completed, report.Busy, report.Elapsed, report.Allocated, report.HeapBefore, report.HeapAfter, report.GoroutinesBefore, report.GoroutinesAfter)
	}
	authorityProxy.setStalled(true)
	clientProxy.setStalled(true)
	send(client, "probe_unavailable", 0)
	receive(client, "unavailable")
	authorityProxy.setStalled(false)
	clientProxy.setStalled(false)
	send(client, "probe_healthy", 0)
	receive(client, "healthy")
	authorityProxy.setBlocked(true)
	clientProxy.setBlocked(true)
	send(client, "probe_unavailable", 0)
	receive(client, "unavailable")
	authorityProxy.setBlocked(false)
	clientProxy.setBlocked(false)
	send(client, "probe_healthy", 0)
	receive(client, "healthy")
	send(client, "seal", 0)
	receive(client, "sealed")
	send(authority, "retire", 0)
	receive(authority, "retired")
	send(client, "probe_retired", 0)
	receive(client, "refused")
	for _, c := range []*child{client, authority} {
		send(c, "stop", 0)
		receive(c, "stopped")
		// Drain the test runner's final PASS output before joining pipes.
		tail, err := io.ReadAll(c.output)
		require.NoError(t, err)
		err = c.cmd.Wait()
		c.joined = true
		require.NoError(t, err, "child output: %s; stderr: %s", tail, c.stderr.String())
	}
}

func TestParticipantProcessHelper(t *testing.T) {
	configPath := os.Getenv(participantProcessEnv)
	if configPath == "" {
		t.Skip("subprocess fixture only")
	}
	var cfg participantProcessConfig
	body, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &cfg))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second+cfg.LoadDuration)
	defer cancel()
	emit := func(op string, port int) {
		require.NoError(t, json.NewEncoder(os.Stdout).Encode(participantProcessMessage{Op: op, Port: port}))
	}
	_, engine := newParticipantTestInventory(t, 4)
	node := localrelay.NewNode(cfg.Node)
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	managerConfig := internode.DefaultManagerConfig()
	managerConfig.Logger = zap.NewNop()
	managerConfig.LocalNodeID = cfg.Node
	managerConfig.BindAddr = "127.0.0.1"
	managerConfig.AutoPort = true
	managerConfig.TLS = cfg.TLS
	managerConfig.AuthenticationKey = []byte("independent-process-fixture-authentication-key")
	managerConfig.SigningKey = cfg.Signing
	managerConfig.RequireAuthentication = true
	managerConfig.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) { return cfg.PeerPublic, id == cfg.Peer }
	managerConfig.AuthorizePeer = func(id cluster.NodeID, _ net.Addr) bool { return id == cfg.Peer }
	manager := internode.NewConnectionManager(managerConfig, nil)
	membership := &participantNativeMembership{local: cluster.NodeInfo{ID: cfg.Node, Addr: "127.0.0.1"}}
	transport := internode.NewService(zap.NewNop(), manager, internode.NewMessageCodec(systempayload.NewTranscoder()), func(pkg *relay.Package) error { return node.Send(pkg) }, bus, membership)
	guard := &topology.NameGuard{}
	registry := NewService(engine, cfg.Node, nil, nil)
	registry.ConfigureStrong(StrongDeps{Incarnation: cfg.Node + "-one", NameGuard: guard, IsLeader: func() bool { return cfg.Node == "authority" }})
	registry.SetNonMember(func() bool { return cfg.Node == "client" })
	require.NoError(t, registry.ConfigureParticipation(4))
	limits := participantSnapshotLimits{MaxEntries: cfg.Claims + 8, MaxValueBytes: cfg.Claims*512 + 8192}
	config := ParticipantEndpointConfig{MaxEntries: limits.MaxEntries, MaxValueBytes: limits.MaxValueBytes, MaxWireBytes: limits.MaxValueBytes * 2, MaxConcurrentRequests: 2, MaxRedirects: 2, RequestTimeout: 3 * time.Second, RefreshInterval: time.Hour}
	endpoint, err := NewParticipantEndpoint(ctx, registry, localrelay.NewRouter(node, transport), func(context.Context) (pid.NodeID, error) { return "authority", nil }, config)
	require.NoError(t, err)
	require.NoError(t, node.RegisterHost(RegistryHostID, endpoint))
	t.Cleanup(func() {
		stop, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		require.NoError(t, endpoint.Stop(stop))
		require.NoError(t, transport.Stop())
	})
	require.NoError(t, transport.Start(ctx))
	manager.AddManagedNode(cfg.Peer)
	emit("listening", manager.GetListenPort())
	input := json.NewDecoder(os.Stdin)
	for {
		var command participantProcessMessage
		require.NoError(t, input.Decode(&command))
		switch command.Op {
		case "connect":
			manager.EnsureConnection(cfg.Peer, "127.0.0.1", command.Port)
			require.Eventually(t, func() bool { return len(manager.ConnectedNodes()) == 1 }, 5*time.Second, 10*time.Millisecond)
			if cfg.Node == "authority" {
				for i := 0; i < cfg.Claims; i++ {
					name := fmt.Sprintf("load-%04d", i)
					owner := mkPID("authority", name)
					value, err := encode(activeValue{Name: name, PID: owner.String()})
					require.NoError(t, err)
					_, err = engine.Set(activeKey(name), value)
					require.NoError(t, err)
				}
			}
			require.NoError(t, endpoint.Start(ctx))
			require.True(t, registry.NameReady())
			emit("ready", 0)
		case "load":
			require.Positive(t, cfg.LoadDuration)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			report := &participantLoadReport{HeapBefore: before.HeapAlloc, GoroutinesBefore: runtime.NumGoroutine()}
			started := time.Now()
			until := started.Add(cfg.LoadDuration)
			for time.Now().Before(until) {
				_, err := registry.refreshParticipant(ctx, registry.strong.participants, endpoint.source, limits)
				if errors.Is(err, errParticipantAuthorityBusy) {
					report.Busy++
					runtime.Gosched()
					continue
				}
				require.NoError(t, err)
				report.Completed++
			}
			report.Elapsed = time.Since(started)
			runtime.ReadMemStats(&after)
			report.Allocated = after.TotalAlloc - before.TotalAlloc
			runtime.GC()
			runtime.ReadMemStats(&after)
			report.HeapAfter = after.HeapAlloc
			report.GoroutinesAfter = runtime.NumGoroutine()
			require.NoError(t, json.NewEncoder(os.Stdout).Encode(participantProcessMessage{Op: "loaded", Report: report}))
		case "probe_unavailable":
			_, err := registry.refreshParticipant(ctx, registry.strong.participants, endpoint.source, limits)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrParticipantRetired)
			require.False(t, registry.NameReady())
			emit("unavailable", 0)
		case "probe_healthy":
			require.Eventually(t, func() bool { return len(manager.ConnectedNodes()) == 1 }, 5*time.Second, 10*time.Millisecond)
			// Exercise the actual coalesced refresh owner to reopen readiness.
			require.Eventually(t, func() bool { registry.requestParticipantRefresh(); return registry.NameReady() }, 5*time.Second, 10*time.Millisecond)
			emit("healthy", 0)
		case "seal":
			require.Equal(t, "client", cfg.Node)
			require.NoError(t, registry.sealParticipantMutations(ctx))
			require.NoError(t, guard.Close(ctx))
			emit("sealed", 0)
		case "retire":
			require.Equal(t, "authority", cfg.Node)
			// Authority-side transition: this is not a remote retirement API test.
			require.NoError(t, registry.strong.participants.retire(ctx, "client", "client-one"))
			emit("retired", 0)
		case "probe_retired":
			_, err := registry.refreshParticipant(ctx, registry.strong.participants, endpoint.source, limits)
			require.ErrorIs(t, err, ErrParticipantRetired)
			require.False(t, registry.NameReady())
			emit("refused", 0)
		case "stop":
			require.NoError(t, endpoint.Stop(ctx))
			require.NoError(t, transport.Stop())
			emit("stopped", 0)
			return
		default:
			t.Fatal(fmt.Sprintf("unknown fixture command %q", command.Op))
		}
	}
}
