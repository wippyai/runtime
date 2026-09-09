// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

const (
	HostID          = "node:topology"
	ControlTopic    = "@topology/control/1"
	MaxControlBytes = 2048
)

type ControlKind string

const (
	MonitorControl   ControlKind = "monitor"
	ReleaseControl   ControlKind = "release"
	InstalledControl ControlKind = "installed"
	ReleasedControl  ControlKind = "released"
	RejectedControl  ControlKind = "rejected"
	MissingControl   ControlKind = "target_missing"
)

var ErrInvalidControl = errors.New("invalid remote topology control")

// Control carries admission and release, not application data or EXIT payloads.
// Its grant still requires target-owner validation. Peer authentication alone
// never authorizes a monitor. Request identity is reused only for exact retries.
type Control struct {
	Watcher   pid.PID
	Target    pid.PID
	RequestID string
	Grant     string
	Kind      ControlKind
}

type wireControl struct {
	Kind      ControlKind `json:"kind"`
	RequestID string      `json:"request_id"`
	Grant     string      `json:"grant"`
	Watcher   string      `json:"watcher"`
	Target    string      `json:"target"`
	Version   int         `json:"version"`
}

func controlActor(p pid.PID) (pid.PID, bool) {
	if !validActorPID(p) {
		return pid.PID{}, false
	}
	return canonicalPID(p), true
}

func controlHex(value string, bytesCount int) bool {
	if len(value) != bytesCount*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (c Control) request() bool { return c.Kind == MonitorControl || c.Kind == ReleaseControl }

func (c Control) valid() bool {
	switch c.Kind {
	case MonitorControl, ReleaseControl, InstalledControl, ReleasedControl, RejectedControl, MissingControl:
	default:
		return false
	}
	_, watcherOK := controlActor(c.Watcher)
	_, targetOK := controlActor(c.Target)
	return watcherOK && targetOK && c.Watcher.Node != c.Target.Node && controlHex(c.RequestID, 16) && controlHex(c.Grant, 32)
}

// EncodeControl creates the one-message native envelope. Callers own the returned
// package until successful relay admission and release it on rejection.
func EncodeControl(c Control) (*relay.Package, error) {
	if !c.valid() {
		return nil, ErrInvalidControl
	}
	watcher, _ := controlActor(c.Watcher)
	target, _ := controlActor(c.Target)
	data, err := json.Marshal(wireControl{Version: 1, Kind: c.Kind, RequestID: c.RequestID, Grant: c.Grant, Watcher: watcher.String(), Target: target.String()})
	if err != nil || len(data) > MaxControlBytes {
		return nil, ErrInvalidControl
	}
	source := watcher
	destination := pid.PID{Node: target.Node, Host: HostID}
	if !c.request() {
		source = pid.PID{Node: target.Node, Host: HostID}
		destination = pid.PID{Node: watcher.Node, Host: HostID}
	}
	return relay.NewPackage(source, destination, ControlTopic, payload.NewPayload(data, payload.JSON)), nil
}

// DecodeControl validates the whole envelope before a receiver may change state.
// It borrows pkg and never consumes it, on either success or rejection. Connection
// lifetime must also be checked at the serialized grant/session admission gate.
func DecodeControl(pkg *relay.Package, localNode pid.NodeID) (Control, error) {
	var c Control
	if pkg == nil || localNode == "" || len(pkg.Messages) != 1 || pkg.Messages[0] == nil {
		return c, ErrInvalidControl
	}
	if pkg.Target.Node != localNode || pkg.Target.Host != HostID || pkg.Target.UniqID != "" {
		return c, ErrInvalidControl
	}
	ingress := pkg.Ingress
	if !ingress.Authenticated || !ingress.IntegrityProtected || ingress.Node == "" || ingress.Node == localNode || ingress.ConnectionClosed == nil {
		return c, ErrInvalidControl
	}
	select {
	case <-ingress.ConnectionClosed:
		return c, ErrInvalidControl
	default:
	}
	message := pkg.Messages[0]
	if message.Topic != ControlTopic || len(message.Payloads) != 1 || message.Payloads[0] == nil || message.Payloads[0].Format() != payload.JSON {
		return c, ErrInvalidControl
	}
	data, ok := message.Payloads[0].Data().([]byte)
	if !ok || len(data) == 0 || len(data) > MaxControlBytes || !utf8.Valid(data) {
		return c, ErrInvalidControl
	}
	wire, err := decodeControlObject(data)
	if err != nil {
		return c, err
	}
	watcher, err := pid.ParsePID(wire.Watcher)
	if err != nil {
		return c, ErrInvalidControl
	}
	target, err := pid.ParsePID(wire.Target)
	if err != nil {
		return c, ErrInvalidControl
	}
	c = Control{Kind: wire.Kind, RequestID: wire.RequestID, Grant: wire.Grant, Watcher: watcher, Target: target}
	if !c.valid() || watcher.String() != wire.Watcher || target.String() != wire.Target {
		return Control{}, ErrInvalidControl
	}
	source := watcher
	destinationNode := target.Node
	if !c.request() {
		source = pid.PID{Node: target.Node, Host: HostID}
		destinationNode = watcher.Node
	}
	if destinationNode != localNode || ingress.Node != source.Node || pkg.Source.Node != source.Node || pkg.Source.Host != source.Host || pkg.Source.UniqID != source.UniqID {
		return Control{}, ErrInvalidControl
	}
	return c, nil
}

// JSON duplicate members and trailing values are invalid, not last-write-wins.
func decodeControlObject(data []byte) (wireControl, error) {
	var value wireControl
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return value, ErrInvalidControl
	}
	seen := make(map[string]bool, 6)
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return wireControl{}, ErrInvalidControl
		}
		seen[key] = true
		switch key {
		case "version":
			err = decoder.Decode(&value.Version)
		case "kind":
			err = decoder.Decode(&value.Kind)
		case "request_id":
			err = decoder.Decode(&value.RequestID)
		case "grant":
			err = decoder.Decode(&value.Grant)
		case "watcher":
			err = decoder.Decode(&value.Watcher)
		case "target":
			err = decoder.Decode(&value.Target)
		default:
			return wireControl{}, ErrInvalidControl
		}
		if err != nil {
			return wireControl{}, ErrInvalidControl
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || len(seen) != 6 || value.Version != 1 {
		return wireControl{}, ErrInvalidControl
	}
	if _, err = decoder.Token(); err != io.EOF {
		return wireControl{}, ErrInvalidControl
	}
	return value, nil
}
