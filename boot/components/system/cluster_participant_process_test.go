// SPDX-License-Identifier: MPL-2.0

package system

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	registryapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	hostapi "github.com/wippyai/runtime/api/service/host"
	topologyapi "github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	bootpkg "github.com/wippyai/runtime/boot"
	metricsboot "github.com/wippyai/runtime/boot/components/metrics"
	"github.com/wippyai/runtime/cluster/internode"
	hostsys "github.com/wippyai/runtime/service/host"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

const bootParticipantProcessEnv = "WIPPY_BOOT_PARTICIPANT_PROCESS_CONFIG"

type bootParticipantProcessConfig struct {
	Node       string
	Role       string
	Internode  int
	Membership int
	Join       string
	Secret     string
	Identity   string
	Trusted    map[string]string
	TLS        internode.ManagerTLSConfig
	DataDir    string
}

type bootParticipantProcessMessage struct {
	DurationNanos int64                   `json:"duration_nanos,omitempty"`
	Name          string                  `json:"name,omitempty"`
	Op            string                  `json:"op"`
	Node          string                  `json:"node,omitempty"`
	Ready         bool                    `json:"ready,omitempty"`
	Leader        bool                    `json:"leader,omitempty"`
	Found         bool                    `json:"found,omitempty"`
	State         globalapi.RegisterState `json:"state,omitempty"`
	Error         string                  `json:"error,omitempty"`
	PID           string                  `json:"pid,omitempty"`
	NameReady     bool                    `json:"name_ready,omitempty"`
}

type bootParticipantChild struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	stderr string
	waited bool
}

// TestBootParticipantNativeProcesses runs three boot-wired Raft members and a
// boot-wired forwarding client in separate OS processes. The client's Strong
// registration must cross authenticated internode transport and commit on the
// member Raft group. A leader crash must preserve active claims and allow a
// fresh Consistent mutation through the survivors. Remaining members are killed
// after client retirement: sequential member retirement can lose quorum.
func TestBootParticipantNativeProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("independent boot/native TLS cluster integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newBootParticipantFixture(t)
	// Hold all future child ports until their child is launched, and give the
	// two listeners distinct explicit addresses. An ephemeral internode listener
	// must not take its own (not-yet-bound) membership port.
	ports := make([]int, 8)
	reservations := make([]net.Listener, len(ports))
	for i := range ports {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		reservations[i] = listener
		ports[i] = listener.Addr().(*net.TCPAddr).Port
		t.Cleanup(func() { _ = listener.Close() })
	}
	releasePorts := func(i int) {
		require.NoError(t, reservations[i].Close())
		require.NoError(t, reservations[i+4].Close())
	}
	join := fmt.Sprintf("127.0.0.1:%d", ports[0])
	children := make([]*bootParticipantChild, 0, 4)
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("boot-node-%d", i+1)
		releasePorts(i)
		children = append(children, startBootParticipantChild(t, ctx, bootParticipantProcessConfig{
			Node: id, Membership: ports[i], Internode: ports[i+4], Join: join, Secret: fixture.secret,
			Identity: fixture.identities[id], Trusted: fixture.trusted, TLS: fixture.tls[id],
			DataDir: filepath.Join(t.TempDir(), id),
		}))
	}
	leaders := 0
	for _, child := range children {
		message := receiveBootParticipantMessage(t, child, "ready")
		require.True(t, message.Ready, "member did not report ready: %+v", message)
		require.True(t, message.NameReady, "member naming admission is not ready: %+v", message)
		if message.Leader {
			leaders++
		}
	}
	require.Equal(t, 1, leaders, "server boot must expose one elected Raft leader")
	clientConfig := bootParticipantProcessConfig{
		Node: "boot-client", Role: "client", Membership: ports[3], Internode: ports[7], Join: join,
		Secret: fixture.secret, Identity: fixture.identities["boot-client"],
		Trusted: fixture.trusted, TLS: fixture.tls["boot-client"], DataDir: filepath.Join(t.TempDir(), "boot-client"),
	}
	releasePorts(3)
	client := startBootParticipantChild(t, ctx, clientConfig)
	children = append(children, client)
	message := receiveBootParticipantMessage(t, client, "ready")
	require.True(t, message.Ready, "client did not report ready: %+v", message)
	require.True(t, message.NameReady, "client participant feed did not open naming admission: %+v", message)
	// Four independent callers contend on authoritative accounting over native
	// authenticated transport. Keep this smoke workload bounded; it is not a
	// sustained-load or 100-node throughput benchmark.
	var latencies []time.Duration
	burstStart := time.Now()
	for round := 0; round < 4; round++ {
		for node, child := range children {
			sendBootParticipantMessage(t, child, bootParticipantProcessMessage{Op: "register", Name: fmt.Sprintf("native-burst-%d-%d", node, round), PID: "burst"})
		}
		for _, child := range children {
			result := receiveBootParticipantMessage(t, child, "registered")
			require.Empty(t, result.Error, "concurrent native Strong registration failed")
			require.Equal(t, globalapi.RegisterStateActive, result.State)
			latencies = append(latencies, time.Duration(result.DurationNanos))
		}
	}
	slices.Sort(latencies)
	t.Logf("native Strong burst: %d claims, %d process callers, wall=%s median=%s max=%s (includes outcome acknowledgement)", len(latencies), len(children), time.Since(burstStart), latencies[len(latencies)/2], latencies[len(latencies)-1])
	sendBootParticipantMessage(t, client, bootParticipantProcessMessage{Op: "register"})
	registered := receiveBootParticipantMessage(t, client, "registered")
	require.Empty(t, registered.Error, "client Strong registration failed")
	require.Equal(t, globalapi.RegisterStateActive, registered.State)
	for _, member := range children[:3] {
		sendBootParticipantMessage(t, member, bootParticipantProcessMessage{Op: "lookup"})
		found := receiveBootParticipantMessage(t, member, "lookup")
		require.Empty(t, found.Error, "member lookup failed")
		require.True(t, found.Found, "member did not observe forwarded Strong registration")
		require.Equal(t, (&pid.PID{Node: "boot-client", Host: "native:process", UniqID: "owner"}).String(), found.PID)
	}
	// Explicitly establish a remote observer across the authenticated mesh.
	// Local registry cleanup alone cannot satisfy this assertion.
	sendBootParticipantMessage(t, children[0], bootParticipantProcessMessage{Op: "monitor", Node: "boot-client"})
	monitored := receiveBootParticipantMessage(t, children[0], "monitored")
	require.Empty(t, monitored.Error, "remote actor monitor establishment failed")
	// Complete a registered topology process and let runtime monitors emit exit.
	sendBootParticipantMessage(t, client, bootParticipantProcessMessage{Op: "complete"})
	exited := receiveBootParticipantMessage(t, client, "completed")
	require.Empty(t, exited.Error)
	sendBootParticipantMessage(t, children[0], bootParticipantProcessMessage{Op: "observe_exit"})
	observed := receiveBootParticipantMessage(t, children[0], "observed_exit")
	require.Empty(t, observed.Error)
	require.Equal(t, (&pid.PID{Node: "boot-client", Host: "native:process", UniqID: "owner"}).String(), observed.PID)

	for _, member := range children[:3] {
		sendBootParticipantMessage(t, member, bootParticipantProcessMessage{Op: "lookup_absent"})
		absent := receiveBootParticipantMessage(t, member, "lookup")
		require.Empty(t, absent.Error)
		require.False(t, absent.Found, "remote exit notification did not reap owner")
	}
	sendBootParticipantMessage(t, client, bootParticipantProcessMessage{Op: "stop"})
	stopped := receiveBootParticipantMessage(t, client, "stopped")
	require.Empty(t, stopped.Error, "client graceful participant retirement failed")
	require.NoError(t, waitBootParticipantChild(client), "client child did not exit cleanly")
	client.waited = true
	// Reuse the actual identity and data directory after committed retirement.
	// A consumer must not need a new node name after a clean runtime restart.
	restarted := startBootParticipantChild(t, ctx, clientConfig)
	readyAgain := receiveBootParticipantMessage(t, restarted, "ready")
	require.True(t, readyAgain.NameReady, "cleanly retired identity could not restart")
	sendBootParticipantMessage(t, restarted, bootParticipantProcessMessage{Op: "register", PID: "replacement"})
	replacement := receiveBootParticipantMessage(t, restarted, "registered")
	require.Empty(t, replacement.Error, "new process could not reclaim explicitly released name")
	require.Equal(t, globalapi.RegisterStateActive, replacement.State)

	// Crash the actual current leader, not a relay adapter. The client must
	// retain its active claim and forward a fresh quorum-backed mutation through
	// the remaining authenticated native mesh after election.
	var crashed *bootParticipantChild
	for _, member := range children[:3] {
		sendBootParticipantMessage(t, member, bootParticipantProcessMessage{Op: "status"})
		status := receiveBootParticipantMessage(t, member, "status")
		if status.Leader {
			require.Nil(t, crashed)
			crashed = member
		}
	}
	require.NotNil(t, crashed)
	require.NoError(t, crashed.cmd.Process.Kill())
	require.Error(t, waitBootParticipantChild(crashed), "hard-killed member unexpectedly exited normally")
	require.Eventually(t, func() bool {
		leaders := 0
		for _, member := range children[:3] {
			if member == crashed {
				continue
			}
			sendBootParticipantMessage(t, member, bootParticipantProcessMessage{Op: "status"})
			status := receiveBootParticipantMessage(t, member, "status")
			if status.Leader {
				leaders++
			}
		}
		return leaders == 1
	}, 20*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		sendBootParticipantMessage(t, restarted, bootParticipantProcessMessage{Op: "register_consistent", Name: "native-after-loss", PID: "after-loss"})
		reply := receiveBootParticipantMessage(t, restarted, "registered")
		return reply.Error == "" && reply.State == globalapi.RegisterStateActive
	}, 25*time.Second, 100*time.Millisecond)
	for _, member := range children[:3] {
		if member == crashed {
			continue
		}
		sendBootParticipantMessage(t, member, bootParticipantProcessMessage{Op: "lookup", Name: "native-after-loss"})
		found := receiveBootParticipantMessage(t, member, "lookup")
		require.Empty(t, found.Error)
		require.True(t, found.Found)
		require.Equal(t, (&pid.PID{Node: "boot-client", Host: "native:process", UniqID: "after-loss"}).String(), found.PID)
		sendBootParticipantMessage(t, member, bootParticipantProcessMessage{Op: "lookup"})
		original := receiveBootParticipantMessage(t, member, "lookup")
		require.Empty(t, original.Error)
		require.True(t, original.Found, "leader crash erased previously committed Strong claim")
		require.Equal(t, (&pid.PID{Node: "boot-client", Host: "native:process", UniqID: "replacement"}).String(), original.PID)
	}

	sendBootParticipantMessage(t, restarted, bootParticipantProcessMessage{Op: "stop"})
	stoppedAgain := receiveBootParticipantMessage(t, restarted, "stopped")
	require.Empty(t, stoppedAgain.Error)
	require.NoError(t, waitBootParticipantChild(restarted))
	restarted.waited = true
	t.Log("member graceful shutdown is intentionally not asserted: final retirement can lose Raft quorum")
}

type bootParticipantFixture struct {
	secret     string
	identities map[string]string
	trusted    map[string]string
	tls        map[string]internode.ManagerTLSConfig
}

func newBootParticipantFixture(t *testing.T) bootParticipantFixture {
	t.Helper()
	secretBytes := make([]byte, 32)
	_, err := rand.Read(secretBytes)
	require.NoError(t, err)
	ids := []string{"boot-node-1", "boot-node-2", "boot-node-3", "boot-client"}
	identities := make(map[string]string, len(ids))
	trusted := make(map[string]string, len(ids))
	for _, id := range ids {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		identities[id] = base64.RawStdEncoding.EncodeToString(priv)
		trusted[id] = base64.RawStdEncoding.EncodeToString(pub)
	}
	return bootParticipantFixture{secret: base64.StdEncoding.EncodeToString(secretBytes), identities: identities, trusted: trusted, tls: newBootParticipantTLS(t, ids)}
}

func newBootParticipantTLS(t *testing.T, ids []string) map[string]internode.ManagerTLSConfig {
	t.Helper()
	dir := t.TempDir()
	rootPub, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "boot-participant-root"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, rootPub, rootPriv)
	require.NoError(t, err)
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), 0600))
	result := make(map[string]internode.ManagerTLSConfig, len(ids))
	for i, id := range ids {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: id}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"}}
		leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, rootTemplate, pub, rootPriv)
		require.NoError(t, err)
		keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
		require.NoError(t, err)
		certPath, keyPath := filepath.Join(dir, id+".pem"), filepath.Join(dir, id+".key")
		require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0600))
		require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
		result[id] = internode.ManagerTLSConfig{Enabled: true, CAFile: caPath, CertFile: certPath, KeyFile: keyPath}
	}
	return result
}

func startBootParticipantChild(t *testing.T, ctx context.Context, cfg bootParticipantProcessConfig) *bootParticipantChild {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	body, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0600))
	child := &bootParticipantChild{cmd: exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBootParticipantProcessHelper$")}
	child.cmd.Env = append(os.Environ(), bootParticipantProcessEnv+"="+path)
	stderrPath := filepath.Join(t.TempDir(), "stderr.log")
	stderrFile, err := os.Create(stderrPath)
	require.NoError(t, err)
	child.stderr = stderrPath
	child.cmd.Stderr = stderrFile
	stdout, err := child.cmd.StdoutPipe()
	require.NoError(t, err)
	child.output = bufio.NewReader(stdout)
	child.input, err = child.cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, child.cmd.Start())
	require.NoError(t, stderrFile.Close())
	t.Cleanup(func() {
		stopBootParticipantChild(child)
		if t.Failed() {
			t.Logf("child diagnostics: %s", bootParticipantStderr(child))
		}
	})
	return child
}

func sendBootParticipantMessage(t *testing.T, child *bootParticipantChild, message bootParticipantProcessMessage) {
	t.Helper()
	require.NoError(t, json.NewEncoder(child.input).Encode(message))
}

func receiveBootParticipantMessage(t *testing.T, child *bootParticipantChild, op string) bootParticipantProcessMessage {
	t.Helper()
	line, err := child.output.ReadBytes('\n')
	require.NoError(t, err, "waiting for %s; stderr: %s", op, bootParticipantStderr(child))
	var message bootParticipantProcessMessage
	if err := json.Unmarshal(line, &message); err != nil {
		remainder, _ := io.ReadAll(child.output)
		stopBootParticipantChild(child)
		t.Fatalf("waiting for %s: %v; child output: %s%s; stderr: %s", op, err, line, remainder, bootParticipantStderr(child))
	}
	require.Equal(t, op, message.Op, "child stderr: %s", bootParticipantStderr(child))
	return message
}

func bootParticipantStderr(child *bootParticipantChild) string {
	if child == nil || child.stderr == "" {
		return ""
	}
	data, _ := os.ReadFile(child.stderr)
	return string(data)
}

func waitBootParticipantChild(child *bootParticipantChild) error {
	if child == nil || child.waited {
		return nil
	}
	err := child.cmd.Wait()
	child.waited = true
	return err
}

func stopBootParticipantChild(child *bootParticipantChild) {
	if child == nil || child.waited {
		return
	}
	_ = child.input.Close()
	if child.cmd.Process != nil && child.cmd.ProcessState == nil {
		_ = child.cmd.Process.Kill()
	}
	_ = waitBootParticipantChild(child)
}

func TestBootParticipantProcessHelper(t *testing.T) {
	path := os.Getenv(bootParticipantProcessEnv)
	if path == "" {
		t.Skip("subprocess fixture only")
	}
	var cfg bootParticipantProcessConfig
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &cfg))
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings := bootParticipantClusterSettings(cfg)
	bootCfg := bootapi.NewConfig(bootapi.WithSection("cluster", settings))
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	ctx, err := bootpkg.NewBootstrapContextWithParent(root, logger, bootCfg)
	require.NoError(t, err)
	loader, err := bootpkg.NewLoader(metricsboot.Metrics(), Topology(), Cluster(), EventualReg(), Raft())
	require.NoError(t, err)
	ctx, err = loader.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, loader.Start(ctx))
	registry := globalapi.GetRegistry(ctx)
	require.NotNil(t, registry)
	raftService := raftapi.GetService(ctx)
	nativeTopology := topologyapi.GetTopology(ctx)
	scheduler := actor.NewScheduler(nil, actor.WithWorkers(1), actor.WithLifecycle(bootMonitorLifecycle{topology: nativeTopology}))
	processHost := hostsys.NewHost(registryapi.NewID("native", "process"), &hostapi.EntryConfig{}, scheduler, nil, nil, logger)
	_, err = processHost.Start(ctx)
	require.NoError(t, err)
	defer processHost.Stop(context.Background())
	nativeNode := relay.GetNode(ctx)
	registrar, ok := nativeNode.(relay.OwnedHostRegistrar)
	require.True(t, ok)
	releaseHost, err := registrar.RegisterOwnedHost("native:process", processHost)
	require.NoError(t, err)
	defer releaseHost()
	inbox := make(bootMonitorInbox, 4)
	releaseInbox, err := registrar.RegisterOwnedHost("native:observer", inbox)
	require.NoError(t, err)
	defer releaseInbox()
	defer func() {
		for {
			select {
			case p := <-inbox:
				relay.ReleasePackage(p)
			default:
				return
			}
		}
	}()
	observer := pid.PID{Node: cfg.Node, Host: "native:observer", UniqID: "watcher"}
	require.NoError(t, nativeTopology.Register(observer))
	actors := make(map[string]*bootMonitorActor)

	emit := func(message bootParticipantProcessMessage) {
		require.NoError(t, json.NewEncoder(os.Stdout).Encode(message))
	}
	nameReady := false
	if composed := topologyapi.GetGlobalRegistry(ctx); composed != nil {
		nameReady = composed.NameReady()
	}
	emit(bootParticipantProcessMessage{Op: "ready", Node: cfg.Node, Ready: true, NameReady: nameReady, Leader: raftService != nil && raftService.IsLeader()})
	input := json.NewDecoder(os.Stdin)
	for {
		var command bootParticipantProcessMessage
		if err := input.Decode(&command); err != nil {
			return
		}
		switch command.Op {
		case "status":
			composed := topologyapi.GetGlobalRegistry(ctx)
			emit(bootParticipantProcessMessage{Op: "status", Node: cfg.Node, NameReady: composed != nil && composed.NameReady(), Leader: raftService != nil && raftService.IsLeader()})
		case "register", "register_consistent":
			owner := pid.PID{Node: cfg.Node, Host: "native:process", UniqID: "owner"}
			if command.PID != "" {
				owner.UniqID = command.PID
			}
			if actors[owner.UniqID] == nil {
				proc := &bootMonitorActor{closed: make(chan struct{})}
				_, err := scheduler.Submit(ctx, owner, proc, "", nil)
				require.NoError(t, err)
				actors[owner.UniqID] = proc
			}
			name := command.Name
			if name == "" {
				name = "native-boot-strong"
			}
			scope := globalapi.Strong
			if command.Op == "register_consistent" {
				scope = globalapi.Consistent
			}
			started := time.Now()
			outcome, err := registry.RegisterScope(ctx, name, owner, scope)
			message := bootParticipantProcessMessage{Op: "registered", State: outcome.State, DurationNanos: time.Since(started).Nanoseconds()}
			if err != nil {
				message.Error = err.Error()
			}
			emit(message)
		case "remove":
			err := registry.Remove(ctx, pid.PID{Node: cfg.Node, Host: "native:process", UniqID: "owner"})
			result := bootParticipantProcessMessage{Op: "removed"}
			if err != nil {
				result.Error = err.Error()
			}
			emit(result)
		case "complete":
			target := pid.PID{Node: cfg.Node, Host: "native:process", UniqID: "owner"}
			require.NoError(t, processHost.Send(relay.NewPackage(observer, target, "fixture:finish", payload.New(true))))
			select {
			case <-actors["owner"].closed:
			case <-time.After(10 * time.Second):
				t.Fatal("native actor did not finish")
			}
			emit(bootParticipantProcessMessage{Op: "completed"})
		case "monitor":
			target := pid.PID{Node: command.Node, Host: "native:process", UniqID: "owner"}
			err := nativeTopology.Monitor(observer, target)
			message := bootParticipantProcessMessage{Op: "monitored"}
			if err != nil {
				message.Error = err.Error()
			}
			emit(message)
		case "observe_exit":
			message := bootParticipantProcessMessage{Op: "observed_exit"}
			select {
			case p := <-inbox:
				fields, ok := p.Messages[0].Payloads[0].Data().(map[string]any)
				if !ok || fields["kind"] != topologyapi.Exit {
					message.Error = "expected ordinary exit event"
				} else {
					owner, ok := fields["from"].(pid.PID)
					if !ok {
						message.Error = "exit missing target identity"
					} else {
						message.PID = owner.String()
					}
					if _, exists := fields["ref"]; exists {
						message.Error = "wire reference leaked to consumer"
					}
				}
				relay.ReleasePackage(p)
			case <-time.After(10 * time.Second):
				message.Error = "remote monitor completion not received"
			}
			emit(message)
		case "lookup", "lookup_absent":
			deadline := time.Now().Add(10 * time.Second)
			var result globalapi.LookupResult
			for time.Now().Before(deadline) {
				name := command.Name
				if name == "" {
					name = "native-boot-strong"
				}
				result, err = registry.Lookup(ctx, name)
				if err == nil && result.Found == (command.Op == "lookup") {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			message := bootParticipantProcessMessage{Op: "lookup", Found: result.Found, PID: result.PID.String()}
			if err != nil {
				message.Error = err.Error()
			}
			emit(message)
		case "stop":
			stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			require.NoError(t, processHost.Stop(stopCtx))
			shutdownErr := loader.Shutdown(stopCtx)
			serviceErr := bootpkg.StopRuntimeServices(stopCtx)
			stopCancel()
			message := bootParticipantProcessMessage{Op: "stopped"}
			if shutdownErr != nil {
				message.Error = shutdownErr.Error()
			} else if serviceErr != nil {
				message.Error = serviceErr.Error()
			}
			emit(message)
			return
		}
	}
}

func bootParticipantClusterSettings(cfg bootParticipantProcessConfig) map[string]any {
	settings := map[string]any{
		"enabled": true, "name": cfg.Node,
		"membership.bind_addr": "127.0.0.1", "membership.bind_port": cfg.Membership,
		"membership.join_addrs": cfg.Join, "membership.secret_key": cfg.Secret,
		"membership.gossip_interval": "100ms", "membership.push_pull_interval": "1s",
		"internode.bind_addr": "127.0.0.1", "internode.bind_port": cfg.Internode, "internode.auto_port": false,
		"internode.identity_key": cfg.Identity, "internode.tls.enabled": cfg.TLS.Enabled,
		"internode.tls.cert_file": cfg.TLS.CertFile, "internode.tls.key_file": cfg.TLS.KeyFile,
		"internode.tls.ca_file": cfg.TLS.CAFile, "raft.registry_backend": "kv",
		"raft.data_dir": cfg.DataDir, "raft.bootstrap_expect": 3,
		"raft.naming.max_entries": 256, "raft.naming.max_value_bytes": 1 << 20,
		"raft.naming.max_wire_bytes": 2 << 20, "raft.naming.request_timeout": "20s",
		"raft.naming.refresh_interval": "250ms",
	}
	if cfg.Role == "client" {
		settings["raft.role"] = "client"
		settings["raft.bootstrap_expect"] = 0
	}
	for id, key := range cfg.Trusted {
		settings["internode.trusted_peer_keys."+id] = key
	}
	return settings
}
