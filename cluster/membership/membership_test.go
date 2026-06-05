// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Test setup helpers

func setupService(_ *testing.T) (*Service, *eventbus.Bus, context.Context, context.CancelFunc) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	ctx, cancel := context.WithCancel(context.Background())

	config := Config{
		NodeName: "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Meta: cluster.NodeMeta{
			"region": "us-east",
			"role":   "worker",
		},
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	return service, bus, ctx, cancel
}

func TestApplyMemberlistTiming_DefaultsAndBadValues(t *testing.T) {
	cfg := memberlist.DefaultLocalConfig()
	applyMemberlistTiming(cfg, Config{})

	assert.Equal(t, DefaultGossipInterval, cfg.GossipInterval)
	assert.Equal(t, DefaultPushPullInterval, cfg.PushPullInterval)
	assert.Equal(t, DefaultDeadNodeReclaimTime, cfg.DeadNodeReclaimTime)

	cfg = memberlist.DefaultLocalConfig()
	applyMemberlistTiming(cfg, Config{
		GossipInterval:      -time.Second,
		PushPullInterval:    -time.Second,
		DeadNodeReclaimTime: -time.Second,
	})

	assert.Equal(t, DefaultGossipInterval, cfg.GossipInterval)
	assert.Equal(t, DefaultPushPullInterval, cfg.PushPullInterval)
	assert.Equal(t, DefaultDeadNodeReclaimTime, cfg.DeadNodeReclaimTime)
}

func TestApplyMemberlistTiming_CustomValues(t *testing.T) {
	cfg := memberlist.DefaultLocalConfig()
	applyMemberlistTiming(cfg, Config{
		GossipInterval:      750 * time.Millisecond,
		PushPullInterval:    10 * time.Second,
		DeadNodeReclaimTime: 2 * time.Minute,
	})

	assert.Equal(t, 750*time.Millisecond, cfg.GossipInterval)
	assert.Equal(t, 10*time.Second, cfg.PushPullInterval)
	assert.Equal(t, 2*time.Minute, cfg.DeadNodeReclaimTime)
}

func generateSecretKey() string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(key)
}

// Service Lifecycle Tests

func TestService_NewService(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	config := Config{
		NodeName: "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
		Meta: cluster.NodeMeta{
			"key": "value",
		},
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	assert.NotNil(t, service)
	assert.NotNil(t, service.logger)
	assert.NotNil(t, service.bus)
	assert.Equal(t, config.NodeName, service.config.NodeName)
	assert.NotNil(t, service.nodes)
	assert.Empty(t, service.nodes)
}

func TestService_Start_Success(t *testing.T) {
	service, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	assert.NotNil(t, service.memberlist)
	assert.NotNil(t, service.ctx)
}

func TestService_Start_WithSecretKey(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secretKey := generateSecretKey()

	config := Config{
		NodeName:     "test-node-secure",
		BindAddr:     "127.0.0.1",
		BindPort:     0,
		SecretString: secretKey,
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	assert.NotNil(t, service.memberlist)
}

func TestService_SendUserMessageRejectsOversizedPayloadBeforeStart(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	err := service.SendUserMessage("peer", 0xE1, make([]byte, ReliableUserMessageMaxPayloadBytes+1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reliable payload too large")
}

func TestService_Start_WithAdvertiseIP(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := Config{
		NodeName:    "test-node-advertise",
		BindAddr:    "127.0.0.1",
		BindPort:    0,
		AdvertiseIP: "192.168.1.100",
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	assert.NotNil(t, service.memberlist)
}

func TestService_Stop(t *testing.T) {
	service, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	err = service.Stop()
	assert.NoError(t, err)
}

func TestService_Stop_BeforeStart(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	err := service.Stop()
	assert.NoError(t, err)
}

func TestService_Start_VeryVerbose(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := Config{
		NodeName:    "test-node-verbose",
		BindAddr:    "127.0.0.1",
		BindPort:    0,
		VeryVerbose: true,
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	assert.NotNil(t, service.memberlist)
}

// Node Management Tests

func TestService_Nodes_EmptyInitially(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	nodes := service.Nodes()
	assert.NotNil(t, nodes)
	assert.Empty(t, nodes)
}

func TestService_Nodes_ConcurrentAccess(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < iterations; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = service.Nodes()
		}()

		go func() {
			defer wg.Done()
			service.mu.Lock()
			service.nodes["test"] = cluster.NodeInfo{ID: "test"}
			service.mu.Unlock()
		}()
	}

	wg.Wait()
}

func TestService_LocalNode_BeforeStart(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	localNode := service.LocalNode()

	assert.Equal(t, "test-node", localNode.ID)
	assert.Equal(t, "127.0.0.1", localNode.Addr)
	assert.Equal(t, "us-east", localNode.Meta["region"])
	assert.Equal(t, "worker", localNode.Meta["role"])
}

func TestService_LocalNode_AfterStart(t *testing.T) {
	service, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	localNode := service.LocalNode()

	assert.Equal(t, "test-node", localNode.ID)
	assert.NotEmpty(t, localNode.Addr)
	assert.Equal(t, "us-east", localNode.Meta["region"])
}

// Event Publishing Tests

func TestEventDelegate_NotifyJoin_PublishesEvent(t *testing.T) {
	service, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	var receivedEvents []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	subscriber, err := eventbus.NewSubscriber(
		ctx,
		bus,
		cluster.System,
		"node.joined",
		func(evt event.Event) {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt)
			mu.Unlock()
			wg.Done()
		},
	)
	require.NoError(t, err)
	defer subscriber.Close()

	ed := &eventDelegate{service: service}
	testNode := &memberlist.Node{
		Name: "remote-node",
		Addr: []byte{192, 168, 1, 100},
		Meta: []byte(`{"role":"worker"}`),
	}

	ed.NotifyJoin(testNode)

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, receivedEvents, 1)
	assert.Equal(t, cluster.System, receivedEvents[0].System)
	assert.Equal(t, cluster.NodeJoined, receivedEvents[0].Kind)
	assert.Equal(t, "remote-node", receivedEvents[0].Path)

	nodeEvent := receivedEvents[0].Data.(cluster.NodeEvent)
	assert.Equal(t, "remote-node", nodeEvent.Node.ID)
	assert.Equal(t, "192.168.1.100:0", nodeEvent.Node.Addr)
	assert.Equal(t, "worker", nodeEvent.Node.Meta["role"])
}

func TestEventDelegate_NotifyLeave_PublishesEvent(t *testing.T) {
	service, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	service.mu.Lock()
	service.nodes["remote-node"] = cluster.NodeInfo{ID: "remote-node"}
	service.mu.Unlock()

	var receivedEvents []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	subscriber, err := eventbus.NewSubscriber(
		ctx,
		bus,
		cluster.System,
		"node.left",
		func(evt event.Event) {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt)
			mu.Unlock()
			wg.Done()
		},
	)
	require.NoError(t, err)
	defer subscriber.Close()

	ed := &eventDelegate{service: service}
	testNode := &memberlist.Node{
		Name: "remote-node",
		Addr: []byte{192, 168, 1, 100},
	}

	ed.NotifyLeave(testNode)

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, receivedEvents, 1)
	assert.Equal(t, cluster.NodeLeft, receivedEvents[0].Kind)
	assert.Equal(t, "remote-node", receivedEvents[0].Path)

	service.mu.RLock()
	_, exists := service.nodes["remote-node"]
	service.mu.RUnlock()
	assert.False(t, exists)
}

func TestEventDelegate_NotifyUpdate_PublishesEvent(t *testing.T) {
	service, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	var receivedEvents []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	subscriber, err := eventbus.NewSubscriber(
		ctx,
		bus,
		cluster.System,
		"node.updated",
		func(evt event.Event) {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt)
			mu.Unlock()
			wg.Done()
		},
	)
	require.NoError(t, err)
	defer subscriber.Close()

	ed := &eventDelegate{service: service}
	testNode := &memberlist.Node{
		Name: "remote-node",
		Addr: []byte{192, 168, 1, 100},
		Meta: []byte(`{"version":"2.0"}`),
	}

	ed.NotifyUpdate(testNode)

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, receivedEvents, 1)
	assert.Equal(t, cluster.NodeUpdated, receivedEvents[0].Kind)

	nodeEvent := receivedEvents[0].Data.(cluster.NodeEvent)
	assert.Equal(t, "2.0", nodeEvent.Node.Meta["version"])
}

func TestEventDelegate_SkipsOwnNode(t *testing.T) {
	service, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	eventCount := 0
	var mu sync.Mutex

	subscriber, err := eventbus.NewSubscriber(
		ctx,
		bus,
		cluster.System,
		"node.*",
		func(_ event.Event) {
			mu.Lock()
			eventCount++
			mu.Unlock()
		},
	)
	require.NoError(t, err)
	defer subscriber.Close()

	ed := &eventDelegate{service: service}
	ownNode := &memberlist.Node{
		Name: "test-node",
		Addr: []byte{127, 0, 0, 1},
	}

	ed.NotifyJoin(ownNode)
	ed.NotifyLeave(ownNode)
	ed.NotifyUpdate(ownNode)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 0, eventCount)
}

func TestEventDelegate_NotifyJoin_UpdatesNodesMap(t *testing.T) {
	service, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	ed := &eventDelegate{service: service}
	testNode := &memberlist.Node{
		Name: "remote-node",
		Addr: []byte{192, 168, 1, 100},
		Meta: []byte(`{"role":"worker"}`),
	}

	ed.NotifyJoin(testNode)

	service.mu.RLock()
	nodeInfo, exists := service.nodes["remote-node"]
	service.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, "remote-node", nodeInfo.ID)
	assert.Equal(t, "192.168.1.100:0", nodeInfo.Addr)
	assert.Equal(t, "worker", nodeInfo.Meta["role"])
}

func TestEventDelegate_NotifyLeave_RemovesFromNodesMap(t *testing.T) {
	service, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	service.mu.Lock()
	service.nodes["remote-node"] = cluster.NodeInfo{ID: "remote-node"}
	service.mu.Unlock()

	ed := &eventDelegate{service: service}
	testNode := &memberlist.Node{
		Name: "remote-node",
		Addr: []byte{192, 168, 1, 100},
	}

	ed.NotifyLeave(testNode)

	service.mu.RLock()
	_, exists := service.nodes["remote-node"]
	service.mu.RUnlock()

	assert.False(t, exists)
}

// Metadata Handling Tests

func TestEventDelegate_ParseNodeMeta_ValidJSON(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	ed := &eventDelegate{service: service}

	meta := map[string]string{
		"region":  "us-west",
		"version": "1.0.0",
	}
	metaBytes, err := json.Marshal(meta)
	require.NoError(t, err)

	parsed := ed.parseNodeMeta(metaBytes)

	assert.Equal(t, "us-west", parsed["region"])
	assert.Equal(t, "1.0.0", parsed["version"])
}

func TestEventDelegate_ParseNodeMeta_InvalidJSON(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	ed := &eventDelegate{service: service}

	invalidJSON := []byte(`{invalid json}`)
	parsed := ed.parseNodeMeta(invalidJSON)

	assert.Equal(t, "{invalid json}", parsed["raw"])
}

func TestEventDelegate_ParseNodeMeta_EmptyBytes(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	ed := &eventDelegate{service: service}

	parsed := ed.parseNodeMeta([]byte{})

	assert.NotNil(t, parsed)
	assert.Empty(t, parsed)
}

func TestDelegate_NodeMeta_ValidJSON(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	meta := d.NodeMeta(512)

	assert.NotEmpty(t, meta)

	var parsed cluster.NodeMeta
	err := json.Unmarshal(meta, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "us-east", parsed["region"])
	assert.Equal(t, "worker", parsed["role"])
}

func TestDelegate_NodeMeta_EmptyMeta(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	config := Config{
		NodeName: "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
		Meta:     cluster.NodeMeta{},
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	meta := d.NodeMeta(512)

	assert.Empty(t, meta)
}

func TestDelegate_NodeMeta_ExceedsLimit(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	largeMeta := cluster.NodeMeta{}
	for i := 0; i < 100; i++ {
		largeMeta[string(rune('a'+i%26))+string(rune(i))] = "very long value string to exceed limit"
	}

	config := Config{
		NodeName: "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
		Meta:     largeMeta,
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	meta := d.NodeMeta(10)

	assert.Empty(t, meta)
}

func TestDelegate_GetBroadcasts(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	broadcasts := d.GetBroadcasts(10, 512)

	assert.Nil(t, broadcasts)
}

func TestDelegate_LocalState(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	state := d.LocalState(false)

	// No user delegates: an empty multiplex stream (zero frames).
	assert.Empty(t, state)
}

func TestDelegate_MergeRemoteState(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	d.MergeRemoteState([]byte(`{"key":"value"}`), true)
}

func TestDelegate_NotifyMsg(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	d.NotifyMsg([]byte("test message"))
}

func TestDelegate_NotifyMsgDropsOversizedReliablePayload(t *testing.T) {
	service, _, _, cancel := setupService(t)
	defer cancel()

	cap := &captureDelegate{kind: 0xE1}
	require.NoError(t, service.RegisterUserDelegate(cap))
	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	payload := make([]byte, ReliableUserMessageMaxPayloadBytes+1)
	wrapped := make([]byte, 0, len(payload)+5)
	wrapped = append(wrapped, cap.kind)
	n := uint32(len(payload))
	wrapped = append(wrapped, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	wrapped = append(wrapped, payload...)

	d.NotifyMsg(wrapped)

	assert.Equal(t, int64(0), cap.rx.Load())
}

func TestDelegate_NotifyMsg_VeryVerbose(_ *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	config := Config{
		NodeName:    "test-node",
		BindAddr:    "127.0.0.1",
		BindPort:    7946,
		VeryVerbose: true,
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	d.NotifyMsg([]byte("test message"))
}

func TestDelegate_MergeRemoteState_VeryVerbose(_ *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	config := Config{
		NodeName:    "test-node",
		BindAddr:    "127.0.0.1",
		BindPort:    7946,
		VeryVerbose: true,
	}

	service := NewService(config, bus, logger, nil, nil, nil)
	d := newDelegate(service, memberlist.DefaultLocalConfig().RetransmitMult)

	d.MergeRemoteState([]byte(`{"key":"value"}`), true)
}

// Secret Key Tests

func TestService_LoadSecretKey_FromString(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	secretKey := generateSecretKey()

	config := Config{
		NodeName:     "test-node",
		BindAddr:     "127.0.0.1",
		BindPort:     7946,
		SecretString: secretKey,
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	key, err := service.loadSecretKey()
	require.NoError(t, err)
	assert.Len(t, key, 32)
}

func TestService_LoadSecretKey_FromFile(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.key")

	secretKey := generateSecretKey()
	err := os.WriteFile(secretFile, []byte(secretKey), 0600)
	require.NoError(t, err)

	config := Config{
		NodeName:   "test-node",
		BindAddr:   "127.0.0.1",
		BindPort:   7946,
		SecretFile: secretFile,
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	key, err := service.loadSecretKey()
	require.NoError(t, err)
	assert.Len(t, key, 32)
}

func TestService_LoadSecretKey_FromFileWithWhitespace(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.key")

	secretKey := generateSecretKey()
	err := os.WriteFile(secretFile, []byte("  "+secretKey+"  \n"), 0600)
	require.NoError(t, err)

	config := Config{
		NodeName:   "test-node",
		BindAddr:   "127.0.0.1",
		BindPort:   7946,
		SecretFile: secretFile,
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	key, err := service.loadSecretKey()
	require.NoError(t, err)
	assert.Len(t, key, 32)
}

func TestService_LoadSecretKey_InvalidBase64(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	config := Config{
		NodeName:     "test-node",
		BindAddr:     "127.0.0.1",
		BindPort:     7946,
		SecretString: "not-valid-base64!!!",
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	_, err := service.loadSecretKey()
	assert.Error(t, err)
}

func TestService_LoadSecretKey_FileNotFound(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	config := Config{
		NodeName:   "test-node",
		BindAddr:   "127.0.0.1",
		BindPort:   7946,
		SecretFile: "/nonexistent/secret.key",
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	_, err := service.loadSecretKey()
	assert.Error(t, err)
}

func TestService_LoadSecretKey_NoSecretProvided(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	config := Config{
		NodeName: "test-node",
		BindAddr: "127.0.0.1",
		BindPort: 7946,
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	_, err := service.loadSecretKey()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no secret key provided")
}

func TestService_LoadSecretKey_PrefersFile(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.key")

	fileKey := generateSecretKey()
	err := os.WriteFile(secretFile, []byte(fileKey), 0600)
	require.NoError(t, err)

	stringKey := generateSecretKey()

	config := Config{
		NodeName:     "test-node",
		BindAddr:     "127.0.0.1",
		BindPort:     7946,
		SecretFile:   secretFile,
		SecretString: stringKey,
	}

	service := NewService(config, bus, logger, nil, nil, nil)

	key, err := service.loadSecretKey()
	require.NoError(t, err)

	fileKeyDecoded, _ := base64.StdEncoding.DecodeString(fileKey)
	assert.Equal(t, fileKeyDecoded, key)
}
