// SPDX-License-Identifier: MPL-2.0

// Package processproof exercises the production cluster boot components from
// separate OS processes. It deliberately has no production surface: the Unix
// control socket exists only in the test helper process.
package processproof

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	bootpkg "github.com/wippyai/runtime/boot"
	metricsboot "github.com/wippyai/runtime/boot/components/metrics"
	systemboot "github.com/wippyai/runtime/boot/components/system"
	"go.uber.org/zap"
)

const helperEnv = "WIPPY_PROCESS_PROOF_HELPER"

const processProofStackLimit = 4 << 20

// dumpProcessProofStacks is deliberately bounded: it is only a diagnostic
// path used after a parent observes a teardown stall. It does not alter the
// helper's shutdown behavior.
func dumpProcessProofStacks(phase string) {
	fmt.Fprintf(os.Stderr, "process-proof teardown diagnostic: phase=%s\n", phase)
	stack := make([]byte, processProofStackLimit)
	n := runtime.Stack(stack, true)
	_, _ = os.Stderr.Write(stack[:n])
}

type helperConfig struct {
	FailEarly  bool              `json:"fail_early,omitempty"`
	CleanExit  bool              `json:"clean_exit,omitempty"`
	Node       string            `json:"node"`
	Membership int               `json:"membership_port"`
	Join       string            `json:"join"`
	DataDir    string            `json:"data_dir"`
	TLSCert    string            `json:"tls_cert"`
	TLSKey     string            `json:"tls_key"`
	TLSCA      string            `json:"tls_ca"`
	Secret     string            `json:"secret"`
	Identity   string            `json:"identity"`
	Trusted    map[string]string `json:"trusted"`
	Control    string            `json:"control"`
}

type controlRequest struct {
	Op   string `json:"op"`
	Name string `json:"name,omitempty"`
	PID  string `json:"pid,omitempty"`
}

type controlResponse struct {
	Error      string   `json:"error,omitempty"`
	Members    int      `json:"members,omitempty"`
	State      string   `json:"state,omitempty"`
	Leader     string   `json:"leader,omitempty"`
	Voters     []string `json:"voters,omitempty"`
	Found      bool     `json:"found,omitempty"`
	PID        string   `json:"pid,omitempty"`
	Registered bool     `json:"registered,omitempty"`
	Active     bool     `json:"active,omitempty"`
	Epoch      uint64   `json:"epoch,omitempty"`
	Removed    bool     `json:"removed,omitempty"`
}

type lockedBuffer struct {
	mu sync.Mutex
	b  []byte
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.b = append(b.b, p...)
	b.mu.Unlock()
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.b)
}

func TestMain(m *testing.M) {
	if raw := os.Getenv(helperEnv); raw != "" {
		var cfg helperConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			fmt.Fprintln(os.Stderr, "invalid process-proof helper config:", err)
			os.Exit(2)
		}
		if err := runHelper(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "process-proof helper:", err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestThreeProcessStrongClaimAndRevoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix-domain test control sockets")
	}
	if testing.Short() {
		t.Skip("launches three runtime processes")
	}
	root := t.TempDir()
	shared := makeCredentials(t, root)
	ports := []int{freeTCPPort(t), freeTCPPort(t), freeTCPPort(t)}
	require.NotEqual(t, ports[0], ports[1])
	require.NotEqual(t, ports[0], ports[2])
	require.NotEqual(t, ports[1], ports[2])
	nodes := make([]*helperProcess, 0, 3)
	t.Cleanup(func() {
		for _, n := range nodes {
			if err := n.stop(); err != nil {
				t.Errorf("helper %s teardown: %v", n.cfg.Node, err)
			}
		}
	})
	for i := range 3 {
		name := fmt.Sprintf("proof-%d", i+1)
		cfg := helperConfig{
			Node: name, Membership: ports[i], DataDir: filepath.Join(root, name),
			TLSCert: shared.certs[name], TLSKey: shared.keys[name], TLSCA: shared.ca,
			Secret: shared.secret, Identity: shared.identities[name], Trusted: shared.trusted,
			Control: filepath.Join(root, name+".sock"),
		}
		if i > 0 {
			cfg.Join = net.JoinHostPort("127.0.0.1", fmt.Sprint(ports[0]))
		}
		nodes = append(nodes, launchHelper(t, cfg))
	}
	for _, node := range nodes {
		node.waitReady(t)
	}
	deadline := time.Now().Add(35 * time.Second)
	var leader *helperProcess
	converged := false
	var diagnostics []string
	for time.Now().Before(deadline) {
		allMembers, voters := true, 0
		leaders := 0
		var voterSignature string
		var leaderID string
		for _, node := range nodes {
			status, err := node.call(controlRequest{Op: "status"})
			diagnostics = append(diagnostics, fmt.Sprintf("%s members=%d state=%s voters=%v leader=%s err=%v", node.cfg.Node, status.Members, status.State, status.Voters, status.Leader, err))
			if err != nil || status.Members != 3 {
				allMembers = false
				break
			}
			voters += len(status.Voters)
			if voterSignature == "" {
				voterSignature = strings.Join(status.Voters, "\x00")
			} else if voterSignature != strings.Join(status.Voters, "\x00") {
				allMembers = false
			}
			if status.Leader == "" {
				allMembers = false
			} else if leaderID == "" {
				leaderID = status.Leader
			} else if leaderID != status.Leader {
				allMembers = false
			}
			if status.State == raftapi.Leader.String() {
				leader = node
				leaders++
			}
		}
		if allMembers && leader != nil && leaders == 1 && voters == 9 && leaderID != "" {
			converged = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !converged || leader == nil {
		t.Fatalf("three runtime processes did not elect a leader; last observations: %s", strings.Join(diagnostics[max(0, len(diagnostics)-3):], "; "))
	}
	for _, node := range nodes {
		status, err := node.call(controlRequest{Op: "status"})
		require.NoError(t, err)
		require.Equal(t, 3, status.Members)
		require.Len(t, status.Voters, 3)
	}

	name := "process-proof-strong"
	owner := (&pid.PID{Node: leader.cfg.Node, Host: "proof", UniqID: "claim"}).Precomputed()
	want := owner.String()
	claim, err := leader.call(controlRequest{Op: "claim", Name: name, PID: want})
	require.NoError(t, err)
	require.True(t, claim.Registered, claim.Error)
	require.True(t, claim.Active)
	require.Positive(t, claim.Epoch)
	for _, node := range nodes {
		require.Eventually(t, func() bool {
			read, err := node.call(controlRequest{Op: "lookup", Name: name})
			return err == nil && read.Found && read.PID == want
		}, 10*time.Second, 50*time.Millisecond, "node %s did not observe strong owner", node.cfg.Node)
	}

	revoke, err := leader.call(controlRequest{Op: "revoke", Name: name})
	require.NoError(t, err)
	require.True(t, revoke.Removed, revoke.Error)
	for _, node := range nodes {
		require.Eventually(t, func() bool {
			read, err := node.call(controlRequest{Op: "lookup", Name: name})
			return err == nil && !read.Found
		}, 10*time.Second, 50*time.Millisecond, "node %s retained revoked strong owner", node.cfg.Node)
	}
}

func TestHelperEarlyExitIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix-domain test control sockets")
	}
	p := launchHelper(t, helperConfig{FailEarly: true, Control: filepath.Join(t.TempDir(), "early.sock")})
	t.Cleanup(func() { _ = p.stop() })
	require.Eventually(t, func() bool {
		select {
		case err := <-p.done:
			p.done <- err
			return err != nil
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond)
	require.Error(t, p.stop(), "a failed child must not be accepted during teardown")
}

func TestHelperCleanExitWithoutShutdownIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix-domain test control sockets")
	}
	p := launchHelper(t, helperConfig{CleanExit: true, Control: filepath.Join(t.TempDir(), "clean.sock")})
	t.Cleanup(func() { _ = p.stop() })
	require.Eventually(t, func() bool {
		select {
		case err := <-p.done:
			p.done <- err
			return err == nil
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond)
	require.Error(t, p.stop(), "a clean child exit without the shutdown protocol must fail teardown")
}

type helperProcess struct {
	cfg      helperConfig
	cmd      *exec.Cmd
	stderr   lockedBuffer
	mu       sync.Mutex
	done     chan error
	stopOnce sync.Once
	stopErr  error
}

func launchHelper(t *testing.T, cfg helperConfig) *helperProcess {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	p := &helperProcess{cfg: cfg, done: make(chan error, 1)}
	p.cmd = exec.Command(os.Args[0], "-test.run=^$")
	configureHelperCommand(p.cmd)
	p.cmd.Env = append(os.Environ(), helperEnv+"="+string(raw))
	p.cmd.Stderr = &p.stderr
	require.NoError(t, p.cmd.Start())
	go func() { p.done <- p.cmd.Wait() }()
	return p
}

func (p *helperProcess) waitReady(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		select {
		case err := <-p.done:
			p.done <- err
			return false
		default:
		}
		response, err := p.call(controlRequest{Op: "ready"})
		return err == nil && response.Active
	}, 20*time.Second, 25*time.Millisecond, "helper %s did not become runtime-ready: %s", p.cfg.Node, p.stderr.String())
}

func (p *helperProcess) call(req controlRequest) (controlResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	conn, err := net.DialTimeout("unix", p.cfg.Control, time.Second)
	if err != nil {
		return controlResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return controlResponse{}, err
	}
	var response controlResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return controlResponse{}, err
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	return response, nil
}

func (p *helperProcess) stop() error {
	p.stopOnce.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		shutdownResponse, shutdownErr := p.call(controlRequest{Op: "shutdown"})
		select {
		case err := <-p.done:
			if shutdownErr != nil {
				p.stopErr = fmt.Errorf("helper exited without shutdown response: %w (exit: %v; stderr: %s)", shutdownErr, err, p.stderr.String())
			} else if err != nil {
				p.stopErr = err
			} else if !shutdownResponse.Removed {
				p.stopErr = fmt.Errorf("helper exited without acknowledging shutdown; stderr: %s", p.stderr.String())
			}
		case <-time.After(15 * time.Second):
			_ = p.cmd.Process.Signal(helperDiagnosticSignal())
			select {
			case err := <-p.done:
				p.stopErr = fmt.Errorf("shutdown timed out; SIGQUIT helper dump captured: %w; helper stderr: %s", err, p.stderr.String())
			case <-time.After(time.Second):
				_ = terminateHelper(p.cmd)
				p.stopErr = fmt.Errorf("forced helper teardown after shutdown response=%+v error=%v: %w; helper stderr: %s", shutdownResponse, shutdownErr, <-p.done, p.stderr.String())
			}
		}
	})
	return p.stopErr
}

func runHelper(cfg helperConfig) (result error) {
	if cfg.FailEarly {
		return errors.New("intentional process-proof helper failure")
	}
	if cfg.CleanExit {
		return nil
	}
	if err := os.Remove(cfg.Control); err != nil && !os.IsNotExist(err) {
		return err
	}
	ctx, cancel := helperSignalContext(context.Background())
	defer cancel()
	var phaseMu sync.RWMutex
	phase := "runtime active"
	setPhase := func(next string) {
		phaseMu.Lock()
		phase = next
		phaseMu.Unlock()
	}
	getPhase := func() string {
		phaseMu.RLock()
		defer phaseMu.RUnlock()
		return phase
	}
	quit := make(chan os.Signal, 1)
	stopDiagnosticSignal := installHelperDiagnosticSignal(quit)
	diagnosticDone := make(chan struct{})
	go func() {
		select {
		case <-quit:
			dumpProcessProofStacks(getPhase())
		case <-diagnosticDone:
		}
	}()
	defer func() {
		stopDiagnosticSignal()
		close(diagnosticDone)
	}()
	clusterSettings := map[string]any{
		"enabled": true, "name": cfg.Node,
		"membership.bind_addr": "127.0.0.1", "membership.bind_port": cfg.Membership,
		"membership.join_addrs": cfg.Join, "membership.secret_key": cfg.Secret,
		"internode.bind_addr": "127.0.0.1", "internode.bind_port": 0, "internode.auto_port": true,
		"internode.identity_key": cfg.Identity,
		"internode.tls.enabled":  true, "internode.tls.cert_file": cfg.TLSCert, "internode.tls.key_file": cfg.TLSKey, "internode.tls.ca_file": cfg.TLSCA,
		"raft.bootstrap_expect": 3, "raft.max_voters": 3, "raft.data_dir": cfg.DataDir,
	}
	for node, key := range cfg.Trusted {
		clusterSettings["internode.trusted_peer_keys."+node] = key
	}
	bootCfg := bootapi.NewConfig(
		bootapi.WithSection("relay", map[string]any{"node_name": cfg.Node}),
		bootapi.WithSection("cluster", clusterSettings),
	)
	bootCtx, err := bootpkg.NewBootstrapContextWithParent(ctx, zap.NewNop(), bootCfg)
	if err != nil {
		return err
	}
	loader, err := bootpkg.NewLoader(metricsboot.Metrics(), systemboot.Cluster(), systemboot.Topology(), systemboot.Raft())
	if err != nil {
		return err
	}
	bootCtx, err = loader.Load(bootCtx)
	if err != nil {
		if shutdownErr := loader.Shutdown(bootCtx); shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}
		return err
	}
	listener, err := net.Listen("unix", cfg.Control)
	if err != nil {
		if shutdownErr := loader.Shutdown(bootCtx); shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}
		return err
	}
	var handlers sync.WaitGroup
	var connections sync.Map
	var listenerCloseOnce sync.Once
	shutdownRequested := make(chan struct{})
	var shutdownOnce sync.Once
	requestShutdown := func() {
		shutdownOnce.Do(func() {
			close(shutdownRequested)
			cancel()
			listenerCloseOnce.Do(func() { _ = listener.Close() })
		})
	}
	listenerDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			listenerCloseOnce.Do(func() { _ = listener.Close() })
		case <-listenerDone:
		}
	}()
	startDone := make(chan error, 1)
	startComplete := make(chan struct{})
	var startMu sync.RWMutex
	var startErr error
	go func() {
		err := loader.Start(bootCtx)
		startMu.Lock()
		startErr = err
		startMu.Unlock()
		close(startComplete)
		startDone <- err
	}()
	defer func() {
		close(listenerDone)
		setPhase("canceling runtime")
		cancel()
		listenerCloseOnce.Do(func() { _ = listener.Close() })
		connections.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		setPhase("waiting for loader start")
		select {
		case startErr := <-startDone:
			if startErr != nil {
				result = errors.Join(result, startErr)
			}
		case <-time.After(12 * time.Second):
			result = errors.Join(result, errors.New("runtime loader start did not return before shutdown"))
			setPhase("joining control handlers after loader start timeout")
			handlers.Wait()
			return
		}
		setPhase("waiting for control handlers")
		handlers.Wait()
		setPhase("shutting down loader")
		if err := loader.Shutdown(bootCtx); err != nil {
			result = errors.Join(result, err)
		}
		setPhase("teardown complete")
		_ = os.Remove(cfg.Control)
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				select {
				case <-shutdownRequested:
					return nil
				default:
					return errors.New("runtime stopped without a shutdown control request")
				}
			}
			return err
		}
		connections.Store(conn, struct{}{})
		handlers.Add(1)
		go func() {
			serveControl(bootCtx, conn, func() error {
				startMu.RLock()
				defer startMu.RUnlock()
				return startErr
			}, func() bool {
				select {
				case <-startComplete:
					return true
				default:
					return false
				}
			}, requestShutdown)
			connections.Delete(conn)
			handlers.Done()
		}()
	}
}

func serveControl(ctx context.Context, conn net.Conn, startupError func() error, startupComplete func() bool, requestShutdown func()) {
	defer conn.Close()
	var req controlRequest
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	response := controlResponse{}
	if req.Op == "shutdown" {
		response.Removed = true
		if err := json.NewEncoder(conn).Encode(response); err != nil {
			return
		}
		requestShutdown()
		return
	}
	if req.Op == "ready" && !startupComplete() {
		response.Error = "runtime startup is incomplete"
		_ = json.NewEncoder(conn).Encode(response)
		return
	}
	if err := startupError(); err != nil {
		response.Error = "runtime boot failed: " + err.Error()
		_ = json.NewEncoder(conn).Encode(response)
		return
	}
	raft := raftapi.GetService(ctx)
	registry := globalapi.GetRegistry(ctx)
	if raft == nil || registry == nil {
		response.Error = "runtime cluster registry is unavailable"
		_ = json.NewEncoder(conn).Encode(response)
		return
	}
	switch req.Op {
	case "ready":
		response.Active = true
	case "status":
		members := clusterapi.GetMembership(ctx)
		if members != nil {
			// Membership.Nodes is the remote-member map; include this process so
			// the test asserts the actual three-node cluster cardinality.
			response.Members = len(members.Nodes()) + 1
		}
		response.State = raft.State().String()
		leader, _, err := raft.Leader()
		if err == nil {
			response.Leader = leader
		}
		servers, err := raft.GetConfiguration()
		if err != nil {
			response.Error = err.Error()
			break
		}
		for _, server := range servers {
			if server.IsVoter {
				response.Voters = append(response.Voters, server.ID)
			}
		}
		sort.Strings(response.Voters)
	case "claim":
		p, err := pid.ParsePID(req.PID)
		if err == nil {
			opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			outcome, callErr := registry.RegisterScope(opCtx, req.Name, p, globalapi.Strong)
			cancel()
			err = callErr
			response.Active = outcome.State == globalapi.RegisterStateActive
			response.Epoch = outcome.Epoch
		}
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Registered = true
		}
	case "revoke":
		opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		removed, err := registry.UnregisterScope(opCtx, req.Name, globalapi.Strong)
		cancel()
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Removed = removed
		}
	case "lookup":
		opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := registry.Lookup(opCtx, req.Name)
		cancel()
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Found = result.Found
			if result.Found {
				response.PID = result.PID.String()
			}
		}
	default:
		response.Error = "unknown test control operation"
	}
	_ = json.NewEncoder(conn).Encode(response)
}

type credentials struct {
	ca, secret          string
	certs, keys         map[string]string
	identities, trusted map[string]string
}

func makeCredentials(t *testing.T, dir string) credentials {
	t.Helper()
	caPub, caKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "process-proof-ca"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPub, caKey)
	require.NoError(t, err)
	ca := filepath.Join(dir, "tls-ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600))
	secretBytes := make([]byte, 32)
	_, err = rand.Read(secretBytes)
	require.NoError(t, err)
	c := credentials{ca: ca, secret: base64.StdEncoding.EncodeToString(secretBytes), certs: map[string]string{}, keys: map[string]string{}, identities: map[string]string{}, trusted: map[string]string{}}
	for i := range 3 {
		name := fmt.Sprintf("proof-%d", i+1)
		leafPub, leafKey, e := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, e)
		leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
		leafDER, e := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, leafPub, caKey)
		require.NoError(t, e)
		cert := filepath.Join(dir, name+"-tls-cert.pem")
		key := filepath.Join(dir, name+"-tls-key.pem")
		require.NoError(t, os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0600))
		leafKeyDER, e := x509.MarshalPKCS8PrivateKey(leafKey)
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER}), 0600))
		c.certs[name], c.keys[name] = cert, key
		pub, priv, e := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, e)
		c.identities[name] = base64.RawStdEncoding.EncodeToString(priv)
		c.trusted[name] = base64.RawStdEncoding.EncodeToString(pub)
	}
	return c
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
