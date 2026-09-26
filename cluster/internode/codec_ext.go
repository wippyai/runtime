// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"reflect"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

// MsgPack extension tags of the internode wire. Every node runs the same
// table and the cluster upgrades all nodes together, so a tag is never
// reassigned while a cluster runs.
//
//	tag  Go type                       ext body (MsgPack)
//	1    pid.PID                       PID string bytes
//	2    apierror.Chain                [ChainedError...]
//	3    topology.ExitEvent            [at, from, kind, result|nil]
//	                                   result: [[data, format]|nil, Chain|nil]
//	4    topology.CancelEvent          [at, from, kind, reason]
//	5    topology.OutdatedEvent        [registry.ID...]
//	6    topology.MonitorRequestEvent  [at, kind, caller, target]
//	7    topology.MonitorReleaseEvent  [at, kind, caller, target]
//	8    topology.LinkRequestEvent     [at, kind, from, to]
//	9    topology.UnlinkRequestEvent   [at, kind, from, to]
const (
	extTagPID uint64 = iota + 1
	extTagErrorChain
	extTagExitEvent
	extTagCancelEvent
	extTagOutdatedEvent
	extTagMonitorRequest
	extTagMonitorRelease
	extTagLinkRequest
	extTagUnlinkRequest
)

// extRef turns a decoded extension value into the pointer its producers send.
// MsgPack decodes an extension held in an interface as a value of the
// registered type; payload consumers match on pointers.
type extRef interface {
	ref(v any) any
}

type exitEventBody struct {
	_struct struct{} `codec:",toarray"` //nolint:unused // msgpack struct options
	At      time.Time
	Result  *resultBody
	From    pid.PID
	Kind    topology.Kind
}

type resultBody struct {
	_struct struct{} `codec:",toarray"` //nolint:unused // msgpack struct options
	Value   *encodedPayload
	Error   *apierror.Chain
}

type cancelEventBody struct {
	_struct struct{} `codec:",toarray"` //nolint:unused // msgpack struct options
	At      time.Time
	From    pid.PID
	Kind    topology.Kind
	Reason  string
}

// relationBody is the body of the four relationship request events: the
// requesting and the requested PID.
type relationBody struct {
	_struct struct{} `codec:",toarray"` //nolint:unused // msgpack struct options
	At      time.Time
	Kind    topology.Kind
	From    pid.PID
	To      pid.PID
}

// structExt carries T on the wire as the MsgPack encoding of its body B. The
// ext API has no error returns; the handle converts panics raised here into
// Encode and Decode errors.
type structExt[T, B any] struct {
	handle   *codec.MsgpackHandle
	toBody   func(*T) (B, error)
	fromBody func(*B) (T, error)
}

func (x *structExt[T, B]) WriteExt(v any) []byte {
	src, ok := v.(*T)
	if !ok {
		panic(newWireTypeError("ext", v))
	}
	body, err := x.toBody(src)
	if err != nil {
		panic(err)
	}
	var out []byte
	if err := codec.NewEncoderBytes(&out, x.handle).Encode(&body); err != nil {
		panic(err)
	}
	return out
}

func (x *structExt[T, B]) ReadExt(dst any, src []byte) {
	var body B
	if err := codec.NewDecoderBytes(src, x.handle).Decode(&body); err != nil {
		panic(err)
	}
	v, err := x.fromBody(&body)
	if err != nil {
		panic(err)
	}
	*dst.(*T) = v
}

func (x *structExt[T, B]) ref(v any) any {
	t := v.(T)
	return &t
}

func newStructExt[T, B any](h *codec.MsgpackHandle, toBody func(*T) (B, error), fromBody func(*B) (T, error)) *structExt[T, B] {
	return &structExt[T, B]{handle: h, toBody: toBody, fromBody: fromBody}
}

type pidExtension struct{}

func (pidExtension) WriteExt(v any) []byte {
	p, ok := v.(*pid.PID)
	if !ok {
		pv, ok := v.(pid.PID)
		if ok {
			p = &pv
		} else {
			return nil
		}
	}
	return []byte(p.String())
}

func (pidExtension) ReadExt(dst any, src []byte) {
	// The msgpack ext API has no error return. A PID field that fails to
	// parse (corrupt or version-mismatched wire bytes) leaves dst at the
	// zero PID; downstream routing rejects an empty Source/Target rather
	// than acting on a wrong address, so the failure surfaces as a dropped
	// message, not silent misdelivery.
	p, err := pid.ParsePID(string(src))
	if err != nil {
		return
	}
	if pidPtr, ok := dst.(*pid.PID); ok {
		*pidPtr = p
	}
}

// registerExtensions installs the tag table on the codec handle.
func (c *MessageCodec) registerExtensions() error {
	h := c.handle
	exts := []struct {
		ext interface {
			codec.BytesExt
			extRef
		}
		typ reflect.Type
		tag uint64
	}{
		{newStructExt(h, chainToBody, chainFromBody), reflect.TypeOf(apierror.Chain{}), extTagErrorChain},
		{newStructExt(h, c.exitToBody, c.exitFromBody), reflect.TypeOf(topology.ExitEvent{}), extTagExitEvent},
		{newStructExt(h, cancelToBody, cancelFromBody), reflect.TypeOf(topology.CancelEvent{}), extTagCancelEvent},
		{newStructExt(h, outdatedToBody, outdatedFromBody), reflect.TypeOf(topology.OutdatedEvent{}), extTagOutdatedEvent},
		{newStructExt(h, monitorRequestToBody, monitorRequestFromBody), reflect.TypeOf(topology.MonitorRequestEvent{}), extTagMonitorRequest},
		{newStructExt(h, monitorReleaseToBody, monitorReleaseFromBody), reflect.TypeOf(topology.MonitorReleaseEvent{}), extTagMonitorRelease},
		{newStructExt(h, linkRequestToBody, linkRequestFromBody), reflect.TypeOf(topology.LinkRequestEvent{}), extTagLinkRequest},
		{newStructExt(h, unlinkRequestToBody, unlinkRequestFromBody), reflect.TypeOf(topology.UnlinkRequestEvent{}), extTagUnlinkRequest},
	}

	if err := h.SetBytesExt(reflect.TypeOf(pid.PID{}), extTagPID, pidExtension{}); err != nil {
		return newRegisterExtensionError(extTagPID, "pid.PID", err)
	}
	c.refs = make(map[reflect.Type]extRef, len(exts))
	for _, e := range exts {
		if err := h.SetBytesExt(e.typ, e.tag, e.ext); err != nil {
			return newRegisterExtensionError(e.tag, e.typ.String(), err)
		}
		c.refs[e.typ] = e.ext
	}
	return nil
}

func chainToBody(c *apierror.Chain) ([]apierror.ChainedError, error) {
	return c.Errors, nil
}

// chainFromBody rejects an empty chain: FromChain maps it to a nil
// *RichError, which is a non-nil error interface.
func chainFromBody(b *[]apierror.ChainedError) (apierror.Chain, error) {
	if len(*b) == 0 {
		return apierror.Chain{}, newEmptyErrorChainError()
	}
	return apierror.Chain{Errors: *b}, nil
}

func (c *MessageCodec) exitToBody(e *topology.ExitEvent) (exitEventBody, error) {
	body := exitEventBody{At: e.At, From: e.From, Kind: e.Kind}
	if e.Result == nil {
		return body, nil
	}
	result := &resultBody{Error: apierror.BuildChain(e.Result.Error)}
	if e.Result.Value != nil {
		value, err := c.encodePayload(e.Result.Value)
		if err != nil {
			return body, err
		}
		result.Value = &value
	}
	body.Result = result
	return body, nil
}

func (c *MessageCodec) exitFromBody(b *exitEventBody) (topology.ExitEvent, error) {
	event := topology.ExitEvent{At: b.At, From: b.From, Kind: b.Kind}
	if b.Result == nil {
		return event, nil
	}
	result := &runtime.Result{}
	if b.Result.Error != nil {
		result.Error = apierror.FromChain(b.Result.Error)
	}
	if b.Result.Value != nil {
		value, err := c.decodePayload(*b.Result.Value)
		if err != nil {
			return event, err
		}
		result.Value = value
	}
	event.Result = result
	return event, nil
}

func cancelToBody(e *topology.CancelEvent) (cancelEventBody, error) {
	return cancelEventBody{At: e.At, From: e.From, Kind: e.Kind, Reason: e.Reason}, nil
}

func cancelFromBody(b *cancelEventBody) (topology.CancelEvent, error) {
	return topology.CancelEvent{At: b.At, From: b.From, Kind: b.Kind, Reason: b.Reason}, nil
}

func outdatedToBody(e *topology.OutdatedEvent) ([]registry.ID, error) {
	return e.Sources, nil
}

func outdatedFromBody(b *[]registry.ID) (topology.OutdatedEvent, error) {
	return topology.OutdatedEvent{Sources: *b}, nil
}

func monitorRequestToBody(e *topology.MonitorRequestEvent) (relationBody, error) {
	return relationBody{At: e.At, Kind: e.Kind, From: e.Caller, To: e.Target}, nil
}

func monitorRequestFromBody(b *relationBody) (topology.MonitorRequestEvent, error) {
	return topology.MonitorRequestEvent{At: b.At, Kind: b.Kind, Caller: b.From, Target: b.To}, nil
}

func monitorReleaseToBody(e *topology.MonitorReleaseEvent) (relationBody, error) {
	return relationBody{At: e.At, Kind: e.Kind, From: e.Caller, To: e.Target}, nil
}

func monitorReleaseFromBody(b *relationBody) (topology.MonitorReleaseEvent, error) {
	return topology.MonitorReleaseEvent{At: b.At, Kind: b.Kind, Caller: b.From, Target: b.To}, nil
}

func linkRequestToBody(e *topology.LinkRequestEvent) (relationBody, error) {
	return relationBody{At: e.At, Kind: e.Kind, From: e.From, To: e.To}, nil
}

func linkRequestFromBody(b *relationBody) (topology.LinkRequestEvent, error) {
	return topology.LinkRequestEvent{At: b.At, Kind: b.Kind, From: b.From, To: b.To}, nil
}

func unlinkRequestToBody(e *topology.UnlinkRequestEvent) (relationBody, error) {
	return relationBody{At: e.At, Kind: e.Kind, From: e.From, To: e.To}, nil
}

func unlinkRequestFromBody(b *relationBody) (topology.UnlinkRequestEvent, error) {
	return topology.UnlinkRequestEvent{At: b.At, Kind: b.Kind, From: b.From, To: b.To}, nil
}
