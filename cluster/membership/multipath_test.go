// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type routeProbeTransport struct {
	memberlist.NodeAwareTransport
	mu     sync.Mutex
	dials  []string
	writes []string
	live   string
}

func (p *routeProbeTransport) DialAddressTimeout(a memberlist.Address, _ time.Duration) (net.Conn, error) {
	p.mu.Lock()
	p.dials = append(p.dials, a.Addr)
	p.mu.Unlock()
	if a.Addr != p.live {
		return nil, errors.New("unreachable")
	}
	one, two := net.Pipe()
	_ = two.Close()
	return one, nil
}

func (p *routeProbeTransport) WriteToAddress(_ []byte, a memberlist.Address) (time.Time, error) {
	p.mu.Lock()
	p.writes = append(p.writes, a.Addr)
	p.mu.Unlock()
	if a.Addr != p.live {
		return time.Time{}, errors.New("unreachable")
	}
	return time.Now(), nil
}

func TestGossipRouteUsesKnownCandidatePool(t *testing.T) {
	live := "127.0.0.1:18555"
	probe := &routeProbeTransport{live: live}
	routed := newCandidateTransport(probe, func(name string) []string {
		require.Equal(t, "peer", name)
		return []string{"192.0.2.1:7946", live}
	})
	addr := memberlist.Address{Name: "peer", Addr: "192.0.2.2:7946"}
	conn, err := routed.DialAddressTimeout(addr, time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	_, err = routed.WriteToAddress([]byte("ping"), addr)
	require.NoError(t, err)
	probe.mu.Lock()
	defer probe.mu.Unlock()
	require.Contains(t, probe.dials, live)
	require.Contains(t, probe.writes, live)
}

func TestMembershipPublishesConfiguredGossipCandidate(t *testing.T) {
	svc := NewService(Config{NodeName: "candidate-publisher", BindAddr: "127.0.0.1", BindPort: 0,
		AdvertiseCandidateList: "100.70.0.69", Meta: cluster.NodeMeta{"role": "test"}},
		eventbus.NewBus(), zap.NewNop(), nil, nil, nil)
	require.NoError(t, svc.Start(t.Context()))
	defer svc.Stop()
	encoded := svc.LocalNode().Meta[internode.MetadataGossipCandidatesV3]
	require.NotEmpty(t, encoded)
	candidates, err := internode.DecodeCandidates(encoded)
	require.NoError(t, err)
	require.Equal(t, "100.70.0.69", candidates[0].Host)
	require.Equal(t, int(svc.memberlist.Load().LocalNode().Port), candidates[0].Port)
}
