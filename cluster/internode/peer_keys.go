// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"crypto/ed25519"

	"github.com/wippyai/runtime/api/cluster"
)

// ResolveMemberKey combines host-selected trust with membership discovery.
// Discovery cannot authorize a key or override a static pin. The caller owns
// the immutable pins map; source is responsible for synchronized live lookup.
func ResolveMemberKey(local, remote cluster.NodeID, pins map[cluster.NodeID]ed25519.PublicKey, source cluster.PeerKeySource, membership cluster.Membership) (ed25519.PublicKey, bool) {
	if remote == "" || remote == local || membership == nil {
		return nil, false
	}
	key, trusted := pins[remote]
	if !trusted && source != nil {
		key, trusted = source(remote)
	}
	if !trusted || len(key) != ed25519.PublicKeySize {
		return nil, false
	}
	for _, node := range membership.Nodes() {
		if node.ID != remote {
			continue
		}
		advertised, err := ParseIdentityPublicKey(node.Meta[MetadataPublicKey])
		if err != nil || !bytes.Equal(advertised, key) {
			return nil, false
		}
		return bytes.Clone(key), true
	}
	return nil, false
}
