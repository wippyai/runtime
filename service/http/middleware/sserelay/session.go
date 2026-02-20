package sserelay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// Session streams relay messages to an HTTP client using SSE.
// It supports managed mode (target pid bound) and detached mode.
type Session struct {
	connectedAt       time.Time
	pidGen            process.PIDGenerator
	node              relay.Node
	topo              topology.Topology
	transcoder        payload.Transcoder
	ctx               context.Context
	host              relay.AttachableReceiver
	closeReason       atomic.Value
	logger            *zap.Logger
	msgCh             chan *relay.Package
	hardTimer         *time.Timer
	idleTimer         *time.Timer
	heartbeatTicker   *time.Ticker
	cancel            context.CancelFunc
	attachCancel      context.CancelFunc
	metadata          map[string]any
	streamPID         pid.PID
	targetPID         pid.PID
	messageTopic      relay.Topic
	hardTimeout       time.Duration
	heartbeatInterval time.Duration
	idleTimeout       time.Duration
	msgCount          atomic.Int64
	closeOnce         sync.Once
	joined            bool
	monitored         bool
	registered        bool
	attached          bool
	hasTarget         bool
}

// NewSession creates a detached SSE relay session.
func NewSession(
	appCtx context.Context,
	config RelayCommand,
	serverID registry.ID,
	host relay.AttachableReceiver,
	node relay.Node,
	topo topology.Topology,
	transcoder payload.Transcoder,
	pidGen process.PIDGenerator,
	logger *zap.Logger,
) (*Session, error) {
	if host == nil {
		return nil, ErrHostRequired
	}
	if node == nil {
		return nil, ErrNodeRequired
	}
	if topo == nil {
		return nil, ErrTopologyRequired
	}
	if transcoder == nil {
		return nil, ErrTranscoderRequired
	}
	if pidGen == nil {
		return nil, ErrPIDGeneratorRequired
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	heartbeatInterval := DefaultHeartbeatInterval
	if config.HeartbeatInterval != "" {
		d, err := parseNonNegativeDuration("heartbeat_interval", config.HeartbeatInterval)
		if err != nil {
			return nil, err
		}
		heartbeatInterval = d
	}

	var idleTimeout time.Duration
	if config.IdleTimeout != "" {
		d, err := parseNonNegativeDuration("idle_timeout", config.IdleTimeout)
		if err != nil {
			return nil, err
		}
		idleTimeout = d
	}

	var hardTimeout time.Duration
	if config.HardTimeout != "" {
		d, err := parseNonNegativeDuration("hard_timeout", config.HardTimeout)
		if err != nil {
			return nil, err
		}
		hardTimeout = d
	}

	ctx, cancel := context.WithCancel(appCtx)
	streamPID := pidGen.Generate(serverID.String())

	messageTopic := MessageTopic
	if config.MessageTopic != "" {
		messageTopic = config.MessageTopic
	}

	s := &Session{
		ctx:               ctx,
		cancel:            cancel,
		host:              host,
		node:              node,
		topo:              topo,
		transcoder:        transcoder,
		pidGen:            pidGen,
		streamPID:         streamPID,
		messageTopic:      messageTopic,
		metadata:          cloneMetadata(config.Metadata),
		connectedAt:       time.Now(),
		heartbeatInterval: heartbeatInterval,
		idleTimeout:       idleTimeout,
		hardTimeout:       hardTimeout,
		msgCh:             make(chan *relay.Package, DefaultChannelCapacity),
		logger:            logger.With(zap.String("stream_pid", streamPID.String())),
	}

	if config.TargetPID != "" {
		targetPID, err := pid.ParsePID(config.TargetPID)
		if err != nil {
			cancel()
			return nil, newTargetPIDError(config.TargetPID, err)
		}
		s.targetPID = targetPID
		s.hasTarget = true
	}

	return s, nil
}

// StreamPID returns the session PID used for relay messaging.
func (s *Session) StreamPID() pid.PID {
	return s.streamPID
}

// CloseReason returns the last close reason.
func (s *Session) CloseReason() string {
	if val := s.closeReason.Load(); val != nil {
		if reason, ok := val.(string); ok {
			return reason
		}
	}
	return ""
}

// Close marks session closed and cancels its context.
func (s *Session) Close(reason string) {
	s.closeOnce.Do(func() {
		s.closeReason.Store(reason)
		s.cancel()
	})
}

// Serve streams SSE events until canceled/closed. It returns an error only if
// the session fails before streaming begins.
func (s *Session) Serve(reqCtx context.Context, w http.ResponseWriter) error {
	if err := s.start(); err != nil {
		s.cleanup()
		return err
	}
	defer s.cleanup()

	enc, err := newSSEEncoder(w)
	if err != nil {
		return err
	}

	if err := s.prepareHeaders(w); err != nil {
		return err
	}

	// Detached start: provide PID to allow late producer attachment.
	if !s.hasTarget {
		if err := s.writeReady(enc); err != nil {
			s.Close("client disconnected")
			return nil
		}
	}

	s.setHeartbeatInterval(s.heartbeatInterval)
	s.setIdleTimeout(s.idleTimeout)
	s.setHardTimeout(s.hardTimeout)

	for {
		heartbeatC := tickerChan(s.heartbeatTicker)
		idleC := timerChan(s.idleTimer)
		hardC := timerChan(s.hardTimer)

		select {
		case <-s.ctx.Done():
			return nil

		case <-reqCtx.Done():
			s.Close("client disconnected")
			return nil

		case <-heartbeatC:
			s.sendHeartbeat()
			if err := enc.writeComment("ping"); err != nil {
				s.Close("client disconnected")
				return nil
			}

		case <-idleC:
			s.writeDoneAndClose(enc, "idle timeout")
			return nil

		case <-hardC:
			s.writeDoneAndClose(enc, "hard timeout")
			return nil

		case pkg, ok := <-s.msgCh:
			if !ok {
				s.writeDoneAndClose(enc, "session mailbox closed")
				return nil
			}
			if pkg == nil {
				continue
			}
			done, activity := s.handlePackage(enc, pkg)
			relay.ReleasePackage(pkg)
			if activity {
				resetTimer(s.idleTimer, s.idleTimeout)
			}
			if done {
				return nil
			}
		}
	}
}

func (s *Session) start() error {
	if err := s.topo.Register(s.streamPID); err != nil {
		return err
	}
	s.registered = true

	detach, err := s.host.Attach(s.streamPID, s.msgCh)
	if err != nil {
		return newAttachError(err)
	}
	s.attachCancel = detach
	s.attached = true

	if s.hasTarget {
		if err := s.attachTarget(s.targetPID); err != nil {
			return err
		}
		s.monitored = true
		s.joined = true
	}

	return nil
}

func (s *Session) prepareHeaders(w http.ResponseWriter) error {
	h := w.Header()
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/event-stream")
	}
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-cache")
	}
	if h.Get("Connection") == "" {
		h.Set("Connection", "keep-alive")
	}
	// Helps disable proxy buffering for long streams.
	if h.Get("X-Accel-Buffering") == "" {
		h.Set("X-Accel-Buffering", "no")
	}

	if rw, ok := w.(*responseWrapper); ok {
		if !rw.wroteHeader {
			rw.WriteHeader(http.StatusOK)
		}
	} else {
		w.WriteHeader(http.StatusOK)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return ErrSSEFlusherUnavailable
	}
	flusher.Flush()
	return nil
}

func (s *Session) writeReady(enc *sseEncoder) error {
	data, err := json.Marshal(ReadyInfo{
		Metadata:     s.metadata,
		StreamPID:    s.streamPID.String(),
		MessageTopic: s.messageTopic,
	})
	if err != nil {
		return newMarshalError("ready payload", err)
	}
	return enc.writeEvent("ready", string(data))
}

func (s *Session) handlePackage(enc *sseEncoder, pkg *relay.Package) (done bool, activity bool) {
	for _, msg := range pkg.Messages {
		if msg == nil {
			continue
		}

		if msg.Topic == topology.TopicEvents && len(msg.Payloads) > 0 {
			if s.handleExitEvent(enc, msg.Payloads) {
				return true, false
			}
			continue
		}

		if msg.Topic == ControlTopic && len(msg.Payloads) > 0 {
			s.handleControlMessage(msg.Payloads[0])
			continue
		}

		if msg.Topic == CloseTopic {
			reason := "closed by server"
			if len(msg.Payloads) > 0 {
				reason = closeReason(msg.Payloads[0], reason)
			}
			s.writeDoneAndClose(enc, reason)
			return true, false
		}

		if !s.shouldForwardTopic(msg.Topic) {
			continue
		}

		for _, p := range msg.Payloads {
			if payload.IsTerminal(p) {
				s.writeDoneAndClose(enc, "terminal payload")
				return true, false
			}

			data, err := s.payloadToEventData(p)
			if err != nil {
				s.logger.Warn("failed to encode payload", zap.Error(err), zap.String("topic", msg.Topic))
				continue
			}

			if err := enc.writeEvent(msg.Topic, data); err != nil {
				s.Close("client disconnected")
				return true, false
			}
			s.msgCount.Add(1)
			activity = true
		}
	}
	return false, activity
}

func (s *Session) handleExitEvent(enc *sseEncoder, payloads []payload.Payload) bool {
	if !s.hasTarget {
		return false
	}

	for _, p := range payloads {
		switch evt := p.Data().(type) {
		case *topology.ExitEvent:
			if evt != nil && evt.From.String() == s.targetPID.String() {
				s.writeDoneAndClose(enc, "target process exited")
				return true
			}
		case topology.ExitEvent:
			if evt.From.String() == s.targetPID.String() {
				s.writeDoneAndClose(enc, "target process exited")
				return true
			}
		}
	}
	return false
}

func (s *Session) shouldForwardTopic(topic string) bool {
	switch topic {
	case ControlTopic, CloseTopic, topology.TopicEvents, JoinTopic, LeaveTopic, HeartbeatTopic:
		return false
	}

	if s.messageTopic == "" || s.messageTopic == "*" {
		return true
	}
	return topic == s.messageTopic
}

func (s *Session) handleControlMessage(p payload.Payload) {
	cmd, raw, err := s.decodeControl(p)
	if err != nil {
		s.logger.Warn("failed to decode control payload", zap.Error(err))
		return
	}

	if _, ok := raw["message_topic"]; ok {
		if cmd.MessageTopic == "" {
			s.messageTopic = MessageTopic
		} else {
			s.messageTopic = cmd.MessageTopic
		}
	}

	if _, ok := raw["metadata"]; ok {
		s.metadata = cloneMetadata(cmd.Metadata)
	}

	if _, ok := raw["heartbeat_interval"]; ok {
		if cmd.HeartbeatInterval == "" {
			s.setHeartbeatInterval(DefaultHeartbeatInterval)
		} else {
			d, err := parseNonNegativeDuration("heartbeat_interval", cmd.HeartbeatInterval)
			if err != nil {
				s.logger.Warn("invalid heartbeat interval in control message",
					zap.String("value", cmd.HeartbeatInterval),
					zap.Error(err))
			} else {
				s.setHeartbeatInterval(d)
			}
		}
	}

	if _, ok := raw["idle_timeout"]; ok {
		if cmd.IdleTimeout == "" {
			s.setIdleTimeout(0)
		} else {
			d, err := parseNonNegativeDuration("idle_timeout", cmd.IdleTimeout)
			if err != nil {
				s.logger.Warn("invalid idle timeout in control message",
					zap.String("value", cmd.IdleTimeout),
					zap.Error(err))
			} else {
				s.setIdleTimeout(d)
			}
		}
	}

	if _, ok := raw["hard_timeout"]; ok {
		if cmd.HardTimeout == "" {
			s.setHardTimeout(0)
		} else {
			d, err := parseNonNegativeDuration("hard_timeout", cmd.HardTimeout)
			if err != nil {
				s.logger.Warn("invalid hard timeout in control message",
					zap.String("value", cmd.HardTimeout),
					zap.Error(err))
			} else {
				s.setHardTimeout(d)
			}
		}
	}

	if _, ok := raw["target_pid"]; ok {
		s.updateTarget(cmd.TargetPID)
	}
}

func (s *Session) decodeControl(p payload.Payload) (RelayCommand, map[string]json.RawMessage, error) {
	var cmd RelayCommand
	var raw map[string]json.RawMessage

	jsonPayload, err := s.payloadToJSONBytes(p)
	if err != nil {
		return cmd, nil, err
	}

	if err := json.Unmarshal(jsonPayload, &cmd); err != nil {
		return cmd, nil, err
	}
	if err := json.Unmarshal(jsonPayload, &raw); err != nil {
		return cmd, nil, err
	}

	return cmd, raw, nil
}

func (s *Session) updateTarget(target string) {
	if target == "" {
		s.detachTarget()
		return
	}

	newTarget, err := pid.ParsePID(target)
	if err != nil {
		s.logger.Warn("invalid target PID in control message", zap.String("target_pid", target), zap.Error(err))
		return
	}

	if s.hasTarget && s.targetPID.String() == newTarget.String() {
		return
	}

	if err := s.attachTarget(newTarget); err != nil {
		s.logger.Warn("failed to attach to new target PID", zap.Error(err))
		return
	}

	oldTarget := s.targetPID
	oldMonitored := s.monitored
	oldJoined := s.joined
	oldHasTarget := s.hasTarget

	s.targetPID = newTarget
	s.hasTarget = true
	s.monitored = true
	s.joined = true

	if oldHasTarget {
		s.detachSpecificTarget(oldTarget, oldMonitored, oldJoined)
	}
}

func (s *Session) detachTarget() {
	if !s.hasTarget {
		return
	}

	s.detachSpecificTarget(s.targetPID, s.monitored, s.joined)

	s.targetPID = pid.Zero()
	s.joined = false
	s.monitored = false
	s.hasTarget = false
}

func (s *Session) attachTarget(target pid.PID) error {
	if err := s.topo.Monitor(s.streamPID, target); err != nil {
		return err
	}
	if err := s.sendJoin(target); err != nil {
		if derr := s.topo.Demonitor(s.streamPID, target); derr != nil {
			s.logger.Warn("failed to rollback monitor after join failure", zap.Error(derr))
		}
		return err
	}
	return nil
}

func (s *Session) detachSpecificTarget(target pid.PID, monitored, joined bool) {
	if monitored {
		if err := s.topo.Demonitor(s.streamPID, target); err != nil {
			s.logger.Warn("failed to demonitor target", zap.Error(err))
		}
	}
	if joined {
		if err := s.sendLeave(target); err != nil {
			s.logger.Warn("failed to send leave", zap.Error(err))
		}
	}
}

func (s *Session) setHeartbeatInterval(d time.Duration) {
	s.heartbeatInterval = d
	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
		s.heartbeatTicker = nil
	}
	if d > 0 {
		s.heartbeatTicker = time.NewTicker(d)
	}
}

func (s *Session) setIdleTimeout(d time.Duration) {
	s.idleTimeout = d
	if s.idleTimer != nil {
		stopAndDrainTimer(s.idleTimer)
		s.idleTimer = nil
	}
	if d > 0 {
		s.idleTimer = time.NewTimer(d)
	}
}

func (s *Session) setHardTimeout(d time.Duration) {
	s.hardTimeout = d
	if s.hardTimer != nil {
		stopAndDrainTimer(s.hardTimer)
		s.hardTimer = nil
	}
	if d > 0 {
		s.hardTimer = time.NewTimer(d)
	}
}

func (s *Session) sendJoin(target pid.PID) error {
	info, err := json.Marshal(JoinInfo{
		ClientPID: s.streamPID.String(),
		Metadata:  s.metadata,
	})
	if err != nil {
		return newMarshalError("join payload", err)
	}
	return s.node.Send(relay.NewPackage(
		s.streamPID,
		target,
		JoinTopic,
		payload.NewPayload(info, payload.JSON),
	))
}

func (s *Session) sendLeave(target pid.PID) error {
	info, err := json.Marshal(JoinInfo{
		ClientPID: s.streamPID.String(),
		Metadata:  s.metadata,
	})
	if err != nil {
		return newMarshalError("leave payload", err)
	}
	return s.node.Send(relay.NewPackage(
		s.streamPID,
		target,
		LeaveTopic,
		payload.NewPayload(info, payload.JSON),
	))
}

func (s *Session) sendHeartbeat() {
	if !s.hasTarget {
		return
	}

	info, err := json.Marshal(HeartbeatInfo{
		ClientPID:    s.streamPID.String(),
		Uptime:       time.Since(s.connectedAt).String(),
		MessageCount: s.msgCount.Load(),
		Metadata:     s.metadata,
	})
	if err != nil {
		s.logger.Warn("failed to marshal heartbeat payload", zap.Error(err))
		return
	}

	if err := s.node.Send(relay.NewPackage(
		s.streamPID,
		s.targetPID,
		HeartbeatTopic,
		payload.NewPayload(info, payload.JSON),
	)); err != nil {
		s.logger.Warn("failed to send heartbeat", zap.Error(err))
	}
}

func (s *Session) writeDoneAndClose(enc *sseEncoder, reason string) {
	data, err := json.Marshal(map[string]string{"reason": reason})
	if err == nil {
		_ = enc.writeEvent("done", string(data))
	}
	s.Close(reason)
}

func (s *Session) payloadToEventData(p payload.Payload) (string, error) {
	switch p.Format() {
	case payload.String:
		if str, ok := p.Data().(string); ok {
			return str, nil
		}
		return fmt.Sprintf("%v", p.Data()), nil

	case payload.JSON:
		switch v := p.Data().(type) {
		case []byte:
			return string(v), nil
		case string:
			return v, nil
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return "", newMarshalError("json payload", err)
			}
			return string(b), nil
		}

	case payload.Bytes:
		b, ok := p.Data().([]byte)
		if !ok {
			return "", ErrExpectedBytesPayload
		}
		return base64.StdEncoding.EncodeToString(b), nil

	default:
		pj, err := s.transcoder.Transcode(p, payload.JSON)
		if err != nil {
			return "", newTranscodeError(err)
		}
		switch v := pj.Data().(type) {
		case []byte:
			return string(v), nil
		case string:
			return v, nil
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return "", newMarshalError("transcoded payload", err)
			}
			return string(b), nil
		}
	}
}

func (s *Session) payloadToJSONBytes(p payload.Payload) ([]byte, error) {
	switch p.Format() {
	case payload.JSON:
		switch v := p.Data().(type) {
		case []byte:
			return v, nil
		case string:
			return []byte(v), nil
		default:
			return json.Marshal(v)
		}
	default:
		tp, err := s.transcoder.Transcode(p, payload.JSON)
		if err != nil {
			return nil, err
		}
		switch v := tp.Data().(type) {
		case []byte:
			return v, nil
		case string:
			return []byte(v), nil
		default:
			return json.Marshal(v)
		}
	}
}

func (s *Session) cleanup() {
	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
		s.heartbeatTicker = nil
	}
	if s.idleTimer != nil {
		stopAndDrainTimer(s.idleTimer)
		s.idleTimer = nil
	}
	if s.hardTimer != nil {
		stopAndDrainTimer(s.hardTimer)
		s.hardTimer = nil
	}

	s.detachTarget()

	if s.registered {
		s.topo.Complete(s.streamPID, &runtime.Result{
			Value: payload.NewString("sse relay closed"),
		})
		s.registered = false
	}

	if s.attached {
		if s.attachCancel != nil {
			s.attachCancel()
			s.attachCancel = nil
		} else {
			s.host.Detach(s.streamPID)
		}
		s.attached = false
	}

	s.cancel()
}

func closeReason(p payload.Payload, fallback string) string {
	if p == nil {
		return fallback
	}
	switch v := p.Data().(type) {
	case string:
		if v == "" {
			return fallback
		}
		return v
	case []byte:
		if len(v) == 0 {
			return fallback
		}
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil || len(b) == 0 {
			return fallback
		}
		return string(b)
	}
}

func tickerChan(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func timerChan(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func stopAndDrainTimer(t *time.Timer) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if t == nil || d <= 0 {
		return
	}
	stopAndDrainTimer(t)
	t.Reset(d)
}

func parseNonNegativeDuration(field, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, newParseDurationError(field, raw, err)
	}
	if d < 0 {
		return 0, newNegativeDurationError(field, raw)
	}
	return d, nil
}

func cloneMetadata(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
