// SPDX-License-Identifier: MPL-2.0
package kvbacked

import "github.com/wippyai/runtime/api/pid"

const participantSnapshotRedirectTopic = "naming.snapshot.redirect"

type participantRedirectError struct{ Peer pid.NodeID }

func (e *participantRedirectError) Error() string { return "participant authority moved to " + e.Peer }

type participantWireRedirect struct {
	Version     uint8  `codec:"v"`
	Correlation uint64 `codec:"c"`
	Incarnation string `codec:"i"`
	Peer        string `codec:"n"`
}

func encodeParticipantRedirect(correlation uint64, incarnation string, peer pid.NodeID, maxBytes int) ([]byte, error) {
	if correlation == 0 || incarnation == "" || peer == "" || maxBytes <= 0 {
		return nil, errParticipantWireInvalid
	}
	return encodeParticipantValue(&participantWireRedirect{participantWireVersion, correlation, incarnation, peer}, maxBytes)
}
func decodeParticipantRedirect(body []byte, maxBytes int) (uint64, string, pid.NodeID, error) {
	if maxBytes <= 0 || len(body) == 0 {
		return 0, "", "", errParticipantWireInvalid
	}
	if len(body) > maxBytes {
		return 0, "", "", errParticipantWireTooLarge
	}
	fields, err := participantWireMapFields(body, 4)
	if err != nil {
		return 0, "", "", err
	}
	if err = participantWireExactKeys(fields, "v", "c", "i", "n"); err != nil {
		return 0, "", "", err
	}
	var value participantWireRedirect
	if err = decodeParticipantUint(fields["v"], &value.Version); err != nil {
		return 0, "", "", err
	}
	if err = decodeParticipantUint(fields["c"], &value.Correlation); err != nil {
		return 0, "", "", err
	}
	if err = decodeParticipantText(fields["i"], &value.Incarnation); err != nil {
		return 0, "", "", err
	}
	if err = decodeParticipantText(fields["n"], &value.Peer); err != nil {
		return 0, "", "", err
	}
	if value.Version != participantWireVersion || value.Correlation == 0 || value.Incarnation == "" || value.Peer == "" {
		return 0, "", "", errParticipantWireInvalid
	}
	return value.Correlation, value.Incarnation, value.Peer, nil
}
