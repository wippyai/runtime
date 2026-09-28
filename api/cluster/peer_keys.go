// SPDX-License-Identifier: MPL-2.0

package cluster

import "crypto/ed25519"

// PeerKeySource resolves identities approved by the native host for new mesh
// handshakes. The host owns enrollment and storage; membership advertisements
// must never populate this source automatically. Calls may be concurrent and
// must return promptly. Return an owned key copy, or false on any lookup error.
//
// Changing the source affects subsequent handshakes, not established connections
// or application grants. Their owners must separately retire those resources.
// Configured static pins take precedence, including the local node's identity.
type PeerKeySource func(NodeID) (ed25519.PublicKey, bool)
