// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/pid"
)

const (
	participantAckPrefix    = registryPrefix + "participant_ack:"
	participantRejectPrefix = registryPrefix + "participant_reject:"
)

var errInvalidRequiredIncarnations = errors.New("invalid required participant incarnations")

// ackKey derives the acknowledgement key for this reservation. Legacy
// node-only headers retain the existing key format. A bound header always uses
// the incarnation-qualified format, including when its map is malformed; its
// caller must validate the header before reading or writing votes.
func (h pendingHeader) ackKey(name string, epoch uint64, node pid.NodeID) string {
	if h.RequiredIncarnations == nil {
		return ackKey(name, epoch, node)
	}
	return participantVoteKey(participantAckPrefix, name, epoch, node, h.RequiredIncarnations[node])
}

// rejectKey derives the rejection key for this reservation. Bound headers never
// fall back to a node-only key, so an old incarnation's vote cannot satisfy a
// reservation for a new incarnation.
func (h pendingHeader) rejectKey(name string, epoch uint64, node pid.NodeID) string {
	if h.RequiredIncarnations == nil {
		return rejectKey(name, epoch, node)
	}
	return participantVoteKey(participantRejectPrefix, name, epoch, node, h.RequiredIncarnations[node])
}

func participantVoteKey(prefix, name string, epoch uint64, node pid.NodeID, incarnation string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return prefix + enc([]byte(name)) + ":" + strconv.FormatUint(epoch, 10) + ":" +
		enc([]byte(node)) + ":" + enc([]byte(incarnation))
}

// validateRequiredIncarnations validates the optional incarnation binding on a
// pending header. A nil map is the legacy node-only record format. A non-nil
// map must describe exactly one nonempty incarnation for every unique required
// node, with no additional map entries.
func validateRequiredIncarnations(hdr pendingHeader) error {
	if hdr.RequiredIncarnations == nil {
		return nil
	}
	if len(hdr.RequiredNodes) == 0 {
		return fmt.Errorf("%w: bound reservation has no required nodes", errInvalidRequiredIncarnations)
	}
	if len(hdr.RequiredIncarnations) != len(hdr.RequiredNodes) {
		return fmt.Errorf("%w: required nodes=%d incarnations=%d", errInvalidRequiredIncarnations, len(hdr.RequiredNodes), len(hdr.RequiredIncarnations))
	}

	seen := make(map[pid.NodeID]struct{}, len(hdr.RequiredNodes))
	for _, node := range hdr.RequiredNodes {
		if strings.TrimSpace(string(node)) == "" {
			return fmt.Errorf("%w: required node is empty", errInvalidRequiredIncarnations)
		}
		if _, duplicate := seen[node]; duplicate {
			return fmt.Errorf("%w: duplicate required node %q", errInvalidRequiredIncarnations, node)
		}
		seen[node] = struct{}{}
		incarnation, ok := hdr.RequiredIncarnations[node]
		if !ok || strings.TrimSpace(incarnation) == "" {
			return fmt.Errorf("%w: missing incarnation for node %q", errInvalidRequiredIncarnations, node)
		}
	}
	for node := range hdr.RequiredIncarnations {
		if _, required := seen[node]; !required {
			return fmt.Errorf("%w: extra incarnation for node %q", errInvalidRequiredIncarnations, node)
		}
	}
	return nil
}

// participantVoteName decodes only the routing hint. Reconciliation then reads
// the authoritative pending record; event bytes never authorize a vote.
func participantVoteName(key string) (string, bool) {
	for _, prefix := range []string{participantAckPrefix, participantRejectPrefix} {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			encoded, _, found := strings.Cut(rest, ":")
			if !found {
				return "", false
			}
			name, err := base64.RawURLEncoding.DecodeString(encoded)
			return string(name), err == nil && len(name) > 0
		}
	}
	return "", false
}
