// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
)

func TestMemberKeyNeedsHostTrustAndMatchingAdvertisement(t *testing.T) {
	key := ed25519.PublicKey(bytes.Repeat([]byte{1}, ed25519.PublicKeySize))
	other := ed25519.PublicKey(bytes.Repeat([]byte{2}, ed25519.PublicKeySize))
	members := &mockMembership{nodes: []cluster.NodeInfo{{ID: "peer", Meta: cluster.NodeMeta{
		MetadataPublicKey: base64.RawStdEncoding.EncodeToString(key),
	}}}}
	approved := false
	source := cluster.PeerKeySource(func(id string) (ed25519.PublicKey, bool) {
		require.Equal(t, "peer", id)
		return bytes.Clone(key), approved
	})
	_, ok := ResolveMemberKey("owner", "peer", nil, source, members)
	require.False(t, ok, "gossip alone must not authorize")
	approved = true
	resolved, ok := ResolveMemberKey("owner", "peer", nil, source, members)
	require.True(t, ok)
	require.Equal(t, key, resolved)
	resolved[0] = 9
	require.Equal(t, byte(1), key[0], "caller cannot mutate trust")
	approved = false
	_, ok = ResolveMemberKey("owner", "peer", nil, source, members)
	require.False(t, ok, "removed approval denies subsequent handshakes")
	approved = true
	_, ok = ResolveMemberKey("owner", "peer", map[string]ed25519.PublicKey{"peer": other}, source, members)
	require.False(t, ok, "dynamic source cannot override a static pin")
	for _, id := range []string{"", "owner"} {
		_, ok = ResolveMemberKey("owner", id, nil, source, members)
		require.False(t, ok)
	}
	_, ok = ResolveMemberKey("owner", "peer", nil, source, nil)
	require.False(t, ok)
	members.nodes[0].Meta[MetadataPublicKey] = base64.RawStdEncoding.EncodeToString(other)
	_, ok = ResolveMemberKey("owner", "peer", nil, source, members)
	require.False(t, ok, "advertised key must match approved identity")
}
