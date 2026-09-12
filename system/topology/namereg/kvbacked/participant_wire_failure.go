// SPDX-License-Identifier: MPL-2.0

package kvbacked

import "errors"

const participantSnapshotFailureTopic = "naming.snapshot.failure"
const (
	participantUnavailable uint8 = iota + 1
	participantBusy
	participantIncarnationConflict
	participantSnapshotTooLarge
	participantRetired
)

var errParticipantUnavailable = errors.New("participant authority unavailable")

type participantWireFailure struct {
	Version     uint8  `codec:"v"`
	Correlation uint64 `codec:"c"`
	Code        uint8  `codec:"x"`
	Incarnation string `codec:"i"`
}

func encodeParticipantFailure(correlation uint64, incarnation string, cause error, maxBytes int) ([]byte, error) {
	if correlation == 0 || incarnation == "" || cause == nil || maxBytes <= 0 {
		return nil, errParticipantWireInvalid
	}
	code := participantUnavailable
	switch {
	case errors.Is(cause, errParticipantAuthorityBusy):
		code = participantBusy
	case errors.Is(cause, ErrParticipantIncarnationConflict):
		code = participantIncarnationConflict
	case errors.Is(cause, ErrParticipantRetired):
		code = participantRetired
	case errors.Is(cause, errParticipantWireTooLarge):
		code = participantSnapshotTooLarge
	}
	return encodeParticipantValue(&participantWireFailure{Version: participantWireVersion, Correlation: correlation, Code: code, Incarnation: incarnation}, maxBytes)
}
func decodeParticipantFailure(body []byte, maxBytes int) (uint64, string, error) {
	if maxBytes <= 0 || len(body) == 0 {
		return 0, "", errParticipantWireInvalid
	}
	if len(body) > maxBytes {
		return 0, "", errParticipantWireTooLarge
	}
	fields, err := participantWireMapFields(body, 4)
	if err != nil {
		return 0, "", err
	}
	if err := participantWireExactKeys(fields, "v", "c", "x", "i"); err != nil {
		return 0, "", err
	}
	var failure participantWireFailure
	if err := decodeParticipantUint(fields["v"], &failure.Version); err != nil {
		return 0, "", err
	}
	if err := decodeParticipantUint(fields["c"], &failure.Correlation); err != nil {
		return 0, "", err
	}
	if err := decodeParticipantUint(fields["x"], &failure.Code); err != nil {
		return 0, "", err
	}
	if err := decodeParticipantText(fields["i"], &failure.Incarnation); err != nil {
		return 0, "", err
	}
	if failure.Incarnation == "" || failure.Version != participantWireVersion || failure.Correlation == 0 {
		return 0, "", errParticipantWireInvalid
	}
	switch failure.Code {
	case participantUnavailable:
		return failure.Correlation, failure.Incarnation, errParticipantUnavailable
	case participantBusy:
		return failure.Correlation, failure.Incarnation, errParticipantAuthorityBusy
	case participantIncarnationConflict:
		return failure.Correlation, failure.Incarnation, ErrParticipantIncarnationConflict
	case participantRetired:
		return failure.Correlation, failure.Incarnation, ErrParticipantRetired
	case participantSnapshotTooLarge:
		return failure.Correlation, failure.Incarnation, errParticipantWireTooLarge
	default:
		return 0, "", errParticipantWireInvalid
	}
}
