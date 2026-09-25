// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewSessionValidation(t *testing.T) {
	cfg := RelayCommand{}
	serverID := registry.NewID("app", "server")
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}

	_, err := NewSession(context.Background(), cfg, serverID, nil, node, topo, tc, pg, zap.NewNop())
	assert.ErrorIs(t, err, ErrHostRequired)

	_, err = NewSession(context.Background(), cfg, serverID, host, nil, topo, tc, pg, zap.NewNop())
	assert.ErrorIs(t, err, ErrNodeRequired)

	_, err = NewSession(context.Background(), cfg, serverID, host, node, nil, tc, pg, zap.NewNop())
	assert.ErrorIs(t, err, ErrTopologyRequired)

	_, err = NewSession(context.Background(), cfg, serverID, host, node, topo, nil, pg, zap.NewNop())
	assert.ErrorIs(t, err, ErrTranscoderRequired)

	_, err = NewSession(context.Background(), cfg, serverID, host, node, topo, tc, nil, zap.NewNop())
	assert.ErrorIs(t, err, ErrPIDGeneratorRequired)

	_, err = NewSession(context.Background(), RelayCommand{TargetPID: "{bad"}, serverID, host, node, topo, tc, pg, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "target")

	_, err = NewSession(context.Background(), RelayCommand{HeartbeatInterval: "-1s"}, serverID, host, node, topo, tc, pg, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative")

	_, err = NewSession(context.Background(), RelayCommand{IdleTimeout: "-1s"}, serverID, host, node, topo, tc, pg, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative")

	_, err = NewSession(context.Background(), RelayCommand{HardTimeout: "-1s"}, serverID, host, node, topo, tc, pg, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative")
}

func TestSessionServeDetachedReadyAndForward(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}

	s, err := NewSession(
		context.Background(),
		RelayCommand{},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		tc,
		pg,
		zap.NewNop(),
	)
	require.NoError(t, err)

	writer := newTestSSEWriter()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(reqCtx, writer)
	}()

	require.Eventually(t, func() bool {
		return host.hasStream(s.StreamPID())
	}, time.Second, 10*time.Millisecond)

	err = host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		MessageTopic,
		payload.NewString("hello"),
	))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return strings.Contains(writer.String(), "data: hello")
	}, time.Second, 10*time.Millisecond)

	cancelReq()
	require.NoError(t, <-done)

	body := writer.String()
	assert.Contains(t, body, "event: ready")
	assert.Contains(t, body, "event: sse.message")
	assert.Contains(t, body, "data: hello")
	assert.True(t, topo.registered(s.StreamPID()))
	assert.True(t, topo.completed(s.StreamPID()))
	assert.False(t, host.hasStream(s.StreamPID()))
}

func TestSessionServeManagedTargetExit(t *testing.T) {
	for _, test := range []struct {
		event  func(pid.PID, *runtime.Result) payload.Payload
		result *runtime.Result
		name   string
	}{
		{
			event: func(target pid.PID, result *runtime.Result) payload.Payload {
				return payload.New(&topology.ExitEvent{From: target, Kind: topology.Exit, Result: result})
			},
			name: "pointer event with successful result",
		},
		{
			event: func(target pid.PID, result *runtime.Result) payload.Payload {
				return payload.New(topology.ExitEvent{From: target, Kind: topology.Exit, Result: result})
			},
			name:   "value event with error result",
			result: &runtime.Result{Error: errors.New("target failed")},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := newMockHost()
			node := newMockNode()
			topo := newMockTopology()
			target := mustPID("{n1@app:llm|target-1}")
			core, logs := observer.New(zap.WarnLevel)
			node.setSendError(LeaveTopic, target, process.ErrProcessNotFound)

			s, err := NewSession(
				context.Background(),
				RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"},
				registry.NewID("app", "server"),
				host,
				node,
				topo,
				&mockTranscoder{},
				&testPIDGen{},
				zap.New(core),
			)
			require.NoError(t, err)
			writer := newTestSSEWriter()
			joinReady := node.notifyOnSend(JoinTopic, target)
			reqCtx, cancelReq := context.WithCancel(context.Background())
			defer cancelReq()
			done := make(chan error, 1)
			go func() { done <- s.Serve(reqCtx, writer) }()
			waitForSignal(t, joinReady, "managed target join")

			require.NoError(t, host.Send(relay.NewPackage(
				pid.Zero(),
				s.StreamPID(),
				topology.TopicEvents,
				test.event(target, test.result),
			)))
			waitSessionDone(t, done)

			assert.Equal(t, 0, node.sendCount(LeaveTopic, target))
			assert.True(t, topo.monitoredBy(s.StreamPID(), target))
			assert.True(t, topo.demonitorCalled(s.StreamPID(), target))
			assert.Contains(t, writer.String(), `"reason":"target process exited"`)
			assert.Empty(t, logs.FilterMessage("failed to send leave").All())
			assertSessionCleaned(t, s, host, topo)
		})
	}
}

func TestSessionServeManagedTargetExitWriteFailure(t *testing.T) {
	t.Run("writer failure", func(t *testing.T) {
		testManagedExitFinalWrite(t, true, false)
	})
	t.Run("request cancellation during write", func(t *testing.T) {
		testManagedExitFinalWrite(t, false, true)
	})
}

func testManagedExitFinalWrite(t *testing.T, failWrite, cancelRequest bool) {
	t.Helper()
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	target := mustPID("{n1@app:llm|target-exit-write}")
	node.setSendError(LeaveTopic, target, process.ErrProcessNotFound)
	s, err := NewSession(
		context.Background(),
		RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"},
		registry.NewID("app", "server"), host, node, topo,
		&mockTranscoder{}, &testPIDGen{}, zap.NewNop(),
	)
	require.NoError(t, err)
	writer := newTestSSEWriter()
	writeStarted := make(chan struct{}, 1)
	writeRelease := make(chan struct{})
	writer.gateNextWrite(writeStarted, writeRelease, failWrite)
	reqCtx, cancelReq := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var releaseOnce sync.Once
	releaseWriter := func() { releaseOnce.Do(func() { close(writeRelease) }) }
	finished := false
	defer func() {
		cancelReq()
		releaseWriter()
		if !finished {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("session did not finish during test cleanup")
			}
		}
	}()
	joinReady := node.notifyOnSend(JoinTopic, target)
	go func() { done <- s.Serve(reqCtx, writer) }()
	waitForSignal(t, joinReady, "managed target join")
	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(), s.StreamPID(), topology.TopicEvents,
		payload.New(&topology.ExitEvent{From: target, Kind: topology.Exit}),
	)))
	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("done event write did not start")
	}
	if cancelRequest {
		cancelReq()
	}
	releaseWriter()
	select {
	case err := <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("session did not finish after final write")
	}
	assert.Equal(t, 0, node.sendCount(LeaveTopic, target))
	assert.Equal(t, "target process exited", s.CloseReason())
	assertSessionCleaned(t, s, host, topo)
}

func TestSessionServeDisconnectMissingTargetWarns(t *testing.T) {
	for _, test := range []struct {
		err  func() error
		name string
	}{
		{err: func() error { return process.ErrProcessNotFound }, name: "bare process not found"},
		{err: func() error { return fmt.Errorf("relay send: %w", process.ErrProcessNotFound) }, name: "wrapped process not found"},
		{err: func() error { return process.ErrProcessClosed }, name: "process closed"},
		{err: func() error { return errors.New("host unavailable") }, name: "host unavailable"},
		{err: func() error { return errors.New("leave failed") }, name: "plain send error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := newMockHost()
			node := newMockNode()
			topo := newMockTopology()
			target := mustPID("{n1@app:llm|target-disconnect}")
			core, logs := observer.New(zap.WarnLevel)
			node.setSendError(LeaveTopic, target, test.err())
			s, err := NewSession(
				context.Background(),
				RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"},
				registry.NewID("app", "server"), host, node, topo,
				&mockTranscoder{}, &testPIDGen{}, zap.New(core),
			)
			require.NoError(t, err)
			reqCtx, cancelReq := context.WithCancel(context.Background())
			defer cancelReq()
			done := make(chan error, 1)
			joinReady := node.notifyOnSend(JoinTopic, target)
			go func() { done <- s.Serve(reqCtx, newTestSSEWriter()) }()
			waitForSignal(t, joinReady, "managed target join")
			cancelReq()
			waitSessionDone(t, done)
			assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
			assert.Equal(t, 1, logs.FilterMessage("failed to send leave").Len())
			assert.Equal(t, "client disconnected", s.CloseReason())
			assertSessionCleaned(t, s, host, topo)
		})
	}
}

func TestSessionServeQueuedUnobservedExitStillWarns(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	target := mustPID("{n1@app:llm|target-queued-exit}")
	core, logs := observer.New(zap.WarnLevel)
	node.setSendError(LeaveTopic, target, process.ErrProcessNotFound)
	s, err := NewSession(
		context.Background(),
		RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"},
		registry.NewID("app", "server"), host, node, topo,
		&mockTranscoder{}, &testPIDGen{}, zap.New(core),
	)
	require.NoError(t, err)
	require.NoError(t, s.start())
	queued := relay.NewPackage(pid.Zero(), s.StreamPID(), topology.TopicEvents,
		payload.New(&topology.ExitEvent{From: target, Kind: topology.Exit}))
	s.msgCh <- queued
	s.Close("client disconnected")
	s.cleanup()
	relay.ReleasePackage(queued)

	assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
	assert.Equal(t, 1, logs.FilterMessage("failed to send leave").Len())
	assert.Equal(t, "client disconnected", s.CloseReason())
	assertSessionCleaned(t, s, host, topo)
}

func TestSessionExitEventPreservesCurrentAttachment(t *testing.T) {
	target := mustPID("{n1@app:llm|target-preserve}")
	other := mustPID("{n1@app:llm|target-other}")
	newTarget := mustPID("{n1@app:llm|target-retargeted}")

	for _, test := range []struct {
		run  func(*testing.T, *Session, *testSSEWriter)
		name string
	}{
		{
			name: "no target",
			run: func(t *testing.T, s *Session, _ *testSSEWriter) {
				enc, err := newSSEEncoder(newTestSSEWriter())
				require.NoError(t, err)
				assert.False(t, s.handleExitEvent(enc, []payload.Payload{payload.New(&topology.ExitEvent{From: other, Kind: topology.Exit})}))
			},
		},
		{
			name: "other PID exit",
			run: func(t *testing.T, s *Session, writer *testSSEWriter) {
				enc, err := newSSEEncoder(writer)
				require.NoError(t, err)
				assert.False(t, s.handleExitEvent(enc, []payload.Payload{payload.New(&topology.ExitEvent{From: other, Kind: topology.Exit})}))
			},
		},
		{
			name: "nil pointer event",
			run: func(t *testing.T, s *Session, writer *testSSEWriter) {
				enc, err := newSSEEncoder(writer)
				require.NoError(t, err)
				var event *topology.ExitEvent
				assert.False(t, s.handleExitEvent(enc, []payload.Payload{payload.New(event)}))
			},
		},
		{
			name: "stale old PID after retarget",
			run: func(t *testing.T, s *Session, writer *testSSEWriter) {
				s.updateTarget(newTarget.String())
				enc, err := newSSEEncoder(writer)
				require.NoError(t, err)
				assert.False(t, s.handleExitEvent(enc, []payload.Payload{payload.New(&topology.ExitEvent{From: target, Kind: topology.Exit})}))
			},
		},
		{
			name: "matching link down",
			run: func(t *testing.T, s *Session, writer *testSSEWriter) {
				enc, err := newSSEEncoder(writer)
				require.NoError(t, err)
				assert.True(t, s.handleExitEvent(enc, []payload.Payload{payload.New(&topology.ExitEvent{From: target, Kind: topology.LinkDown})}))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := newMockHost()
			node := newMockNode()
			topo := newMockTopology()
			var s *Session
			var err error
			if test.name == "no target" {
				s, err = NewSession(context.Background(), RelayCommand{HeartbeatInterval: "0s"},
					registry.NewID("app", "server"), host, node, topo,
					&mockTranscoder{}, &testPIDGen{}, zap.NewNop())
			} else {
				s, err = newManagedTestSession(t, target, host, node, topo, zap.NewNop())
			}
			if test.name == "no target" {
				require.NoError(t, err)
			}
			require.NoError(t, s.start())
			writer := newTestSSEWriter()
			test.run(t, s, writer)
			s.Close("client disconnected")
			s.cleanup()
			wantTarget := target
			if test.name == "stale old PID after retarget" {
				wantTarget = newTarget
				assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
			}
			if test.name == "no target" {
				assert.Equal(t, 0, node.topicCount(LeaveTopic))
			} else {
				assert.Equal(t, 1, node.sendCount(LeaveTopic, wantTarget))
			}
			if test.name == "matching link down" {
				assert.Equal(t, "target process exited", s.CloseReason())
			}
			assertSessionCleaned(t, s, host, topo)
		})
	}
}

func TestSessionManagedClosePathsLeaveTarget(t *testing.T) {
	t.Run("public close reason", func(t *testing.T) {
		host := newMockHost()
		node := newMockNode()
		topo := newMockTopology()
		target := mustPID("{n1@app:llm|target-public-close}")
		s, err := newManagedTestSession(t, target, host, node, topo, zap.NewNop())
		require.NoError(t, err)
		require.NoError(t, s.start())
		s.Close("target process exited")
		s.cleanup()
		assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
		assert.Equal(t, "target process exited", s.CloseReason())
		assertSessionCleaned(t, s, host, topo)
	})

	t.Run("hard timeout", func(t *testing.T) {
		host := newMockHost()
		node := newMockNode()
		topo := newMockTopology()
		target := mustPID("{n1@app:llm|target-timeout}")
		s, err := NewSession(context.Background(), RelayCommand{
			TargetPID: target.String(), HeartbeatInterval: "0s", HardTimeout: "20ms",
		}, registry.NewID("app", "server"), host, node, topo,
			&mockTranscoder{}, &testPIDGen{}, zap.NewNop())
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- s.Serve(context.Background(), newTestSSEWriter()) }()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("session did not finish after hard timeout")
		}
		assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
		assert.Equal(t, "hard timeout", s.CloseReason())
		assertSessionCleaned(t, s, host, topo)
	})

	t.Run("sse close package", func(t *testing.T) {
		host := newMockHost()
		node := newMockNode()
		topo := newMockTopology()
		target := mustPID("{n1@app:llm|target-close-package}")
		s, err := newManagedTestSession(t, target, host, node, topo, zap.NewNop())
		require.NoError(t, err)
		done := make(chan error, 1)
		joinReady := node.notifyOnSend(JoinTopic, target)
		go func() { done <- s.Serve(context.Background(), newTestSSEWriter()) }()
		waitForSignal(t, joinReady, "managed target join")
		require.NoError(t, host.Send(relay.NewPackage(pid.Zero(), s.StreamPID(), CloseTopic, payload.NewString("closed by server"))))
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("session did not finish after close package")
		}
		assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
		assert.Equal(t, "closed by server", s.CloseReason())
		assertSessionCleaned(t, s, host, topo)
	})
}

func TestSessionHeartbeatFailureWarnsWithoutObservedExit(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	target := mustPID("{n1@app:llm|target-heartbeat}")
	core, logs := observer.New(zap.WarnLevel)
	node.setSendError(HeartbeatTopic, target, errors.New("host unavailable"))
	s, err := newManagedTestSession(t, target, host, node, topo, zap.New(core))
	require.NoError(t, err)
	require.NoError(t, s.start())
	s.sendHeartbeat()

	assert.True(t, s.hasTarget)
	assert.True(t, s.joined)
	assert.Equal(t, 1, node.sendCount(HeartbeatTopic, target))
	assert.Equal(t, 1, logs.FilterMessage("failed to send heartbeat").Len())
	s.Close("client disconnected")
	s.cleanup()
	assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
	assertSessionCleaned(t, s, host, topo)
}

func TestSessionFailedReplacementJoinKeepsCurrentTarget(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	oldTarget := mustPID("{n1@app:llm|target-join-old}")
	newTarget := mustPID("{n1@app:llm|target-join-new}")
	core, logs := observer.New(zap.WarnLevel)
	node.setSendError(JoinTopic, newTarget, errors.New("replacement join failed"))
	s, err := newManagedTestSession(t, oldTarget, host, node, topo, zap.New(core))
	require.NoError(t, err)
	require.NoError(t, s.start())

	s.updateTarget(newTarget.String())
	assert.Equal(t, oldTarget.String(), s.targetPID.String())
	assert.True(t, s.hasTarget)
	assert.True(t, s.monitored)
	assert.True(t, s.joined)
	assert.Equal(t, 1, node.sendCount(JoinTopic, newTarget))
	assert.Equal(t, 1, logs.FilterMessage("failed to attach to new target PID").Len())

	s.Close("client disconnected")
	s.cleanup()
	assert.Equal(t, 1, node.sendCount(LeaveTopic, oldTarget))
	assert.Equal(t, 0, node.sendCount(LeaveTopic, newTarget))
	assertSessionCleaned(t, s, host, topo)
}

func TestSessionOldTargetLeaveFailureKeepsReplacement(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	oldTarget := mustPID("{n1@app:llm|target-leave-old}")
	newTarget := mustPID("{n1@app:llm|target-leave-new}")
	core, logs := observer.New(zap.WarnLevel)
	node.setSendError(LeaveTopic, oldTarget, process.ErrProcessNotFound)
	s, err := newManagedTestSession(t, oldTarget, host, node, topo, zap.New(core))
	require.NoError(t, err)
	require.NoError(t, s.start())

	s.updateTarget(newTarget.String())
	assert.Equal(t, newTarget.String(), s.targetPID.String())
	assert.True(t, s.hasTarget)
	assert.True(t, s.joined)
	assert.Equal(t, 1, node.sendCount(LeaveTopic, oldTarget))
	assert.Equal(t, 1, logs.FilterMessage("failed to send leave").Len())
	assert.Empty(t, s.CloseReason())

	s.Close("client disconnected")
	s.cleanup()
	assert.Equal(t, 1, node.sendCount(LeaveTopic, newTarget))
	assertSessionCleaned(t, s, host, topo)
}

func TestSessionObservedExitStillWarnsOnDemonitorFailure(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	target := mustPID("{n1@app:llm|target-demonitor}")
	core, logs := observer.New(zap.WarnLevel)
	topo.demonitorErr = errors.New("demonitor failed")
	node.setSendError(LeaveTopic, target, process.ErrProcessNotFound)
	s, err := newManagedTestSession(t, target, host, node, topo, zap.New(core))
	require.NoError(t, err)
	done := make(chan error, 1)
	joinReady := node.notifyOnSend(JoinTopic, target)
	go func() { done <- s.Serve(context.Background(), newTestSSEWriter()) }()
	waitForSignal(t, joinReady, "managed target join")
	require.NoError(t, host.Send(relay.NewPackage(pid.Zero(), s.StreamPID(), topology.TopicEvents,
		payload.New(&topology.ExitEvent{From: target, Kind: topology.Exit}))))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("session did not finish after target EXIT")
	}
	assert.Equal(t, 1, logs.FilterMessage("failed to demonitor target").Len())
	assertSessionCleaned(t, s, host, topo)
}

func TestSessionControlAttachDetachAndTopicSwitch(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}
	target := mustPID("{n1@app:llm|target-2}")

	s, err := NewSession(
		context.Background(),
		RelayCommand{
			MessageTopic: "first.topic",
		},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		tc,
		pg,
		zap.NewNop(),
	)
	require.NoError(t, err)

	writer := newTestSSEWriter()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(reqCtx, writer)
	}()
	defer func() {
		cancelReq()
		<-done
	}()

	require.Eventually(t, func() bool {
		return host.hasStream(s.StreamPID())
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		"first.topic",
		payload.NewString("first"),
	)))
	require.Eventually(t, func() bool {
		return strings.Contains(writer.String(), "data: first")
	}, time.Second, 10*time.Millisecond)

	control := fmt.Sprintf(`{"target_pid":"%s","message_topic":"second.topic"}`, target.String())
	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		ControlTopic,
		payload.NewPayload([]byte(control), payload.JSON),
	)))
	require.Eventually(t, func() bool {
		return node.hasTopic(JoinTopic)
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		"first.topic",
		payload.NewString("should_not_forward"),
	)))
	time.Sleep(30 * time.Millisecond)
	assert.NotContains(t, writer.String(), "should_not_forward")

	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		"second.topic",
		payload.NewString("second"),
	)))
	require.Eventually(t, func() bool {
		return strings.Contains(writer.String(), "data: second")
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		ControlTopic,
		payload.NewPayload([]byte(`{"target_pid":""}`), payload.JSON),
	)))
	require.Eventually(t, func() bool {
		return node.hasTopic(LeaveTopic)
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, node.sendCount(LeaveTopic, target))
}

func TestSessionControlInvalidDurationsDoNotOverride(t *testing.T) {
	s, err := NewSession(
		context.Background(),
		RelayCommand{
			HeartbeatInterval: "20ms",
			IdleTimeout:       "40ms",
			HardTimeout:       "60ms",
		},
		registry.NewID("app", "server"),
		newMockHost(),
		newMockNode(),
		newMockTopology(),
		&mockTranscoder{},
		&testPIDGen{},
		zap.NewNop(),
	)
	require.NoError(t, err)

	oldHeartbeat := s.heartbeatInterval
	oldIdle := s.idleTimeout
	oldHard := s.hardTimeout

	s.handleControlMessage(payload.NewPayload([]byte(`{
		"heartbeat_interval":"bad",
		"idle_timeout":"-1s",
		"hard_timeout":"bad"
	}`), payload.JSON))

	assert.Equal(t, oldHeartbeat, s.heartbeatInterval)
	assert.Equal(t, oldIdle, s.idleTimeout)
	assert.Equal(t, oldHard, s.hardTimeout)
}

func TestSessionMonitorFailureDoesNotSendLeave(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	topo.monitorErr = errors.New("monitor failed")
	target := mustPID("{n1@app:llm|target-monitor-fail}")

	s, err := NewSession(
		context.Background(),
		RelayCommand{TargetPID: target.String()},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		&mockTranscoder{},
		&testPIDGen{},
		zap.NewNop(),
	)
	require.NoError(t, err)

	err = s.Serve(context.Background(), newTestSSEWriter())
	require.Error(t, err)
	assert.False(t, node.hasTopic(LeaveTopic))
}

func TestSessionUpdateTargetMonitorFailureKeepsCurrentTarget(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	oldTarget := mustPID("{n1@app:llm|target-old}")
	newTarget := mustPID("{n1@app:llm|target-new}")

	s, err := NewSession(
		context.Background(),
		RelayCommand{TargetPID: oldTarget.String()},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		&mockTranscoder{},
		&testPIDGen{},
		zap.NewNop(),
	)
	require.NoError(t, err)
	require.NoError(t, s.start())
	defer s.cleanup()

	joinCount := node.topicCount(JoinTopic)
	topo.monitorErr = errors.New("monitor failed")
	s.updateTarget(newTarget.String())

	assert.Equal(t, oldTarget.String(), s.targetPID.String())
	assert.True(t, s.hasTarget)
	assert.True(t, s.monitored)
	assert.True(t, s.joined)
	assert.Equal(t, joinCount, node.topicCount(JoinTopic))
	assert.False(t, topo.demonitorCalled(s.streamPID, oldTarget))
}

func TestSessionStartAndCleanupUsesAttachCancel(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()

	s, err := NewSession(
		context.Background(),
		RelayCommand{},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		&mockTranscoder{},
		&testPIDGen{},
		zap.NewNop(),
	)
	require.NoError(t, err)
	require.NoError(t, s.start())

	s.cleanup()

	assert.Equal(t, 1, host.cancelCalls())
	assert.Equal(t, 1, host.detachCalls())
}

func TestSessionNewSessionClonesMetadata(t *testing.T) {
	meta := map[string]any{"role": "admin"}

	s, err := NewSession(
		context.Background(),
		RelayCommand{Metadata: meta},
		registry.NewID("app", "server"),
		newMockHost(),
		newMockNode(),
		newMockTopology(),
		&mockTranscoder{},
		&testPIDGen{},
		zap.NewNop(),
	)
	require.NoError(t, err)

	meta["role"] = "user"
	assert.Equal(t, "admin", s.metadata["role"])
}

func TestSessionTimeouts(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}

	t.Run("idle timeout", func(t *testing.T) {
		s, err := NewSession(
			context.Background(),
			RelayCommand{IdleTimeout: "30ms", HeartbeatInterval: "0s"},
			registry.NewID("app", "server"),
			host,
			node,
			topo,
			tc,
			pg,
			zap.NewNop(),
		)
		require.NoError(t, err)

		writer := newTestSSEWriter()
		reqCtx, cancelReq := context.WithCancel(context.Background())
		defer cancelReq()
		done := make(chan error, 1)
		go func() { done <- s.Serve(reqCtx, writer) }()
		require.NoError(t, <-done)
		assert.Equal(t, "idle timeout", s.CloseReason())
		assert.Contains(t, writer.String(), `"reason":"idle timeout"`)
	})

	t.Run("hard timeout", func(t *testing.T) {
		s, err := NewSession(
			context.Background(),
			RelayCommand{IdleTimeout: "200ms", HardTimeout: "30ms", HeartbeatInterval: "0s"},
			registry.NewID("app", "server"),
			host,
			node,
			topo,
			tc,
			pg,
			zap.NewNop(),
		)
		require.NoError(t, err)

		writer := newTestSSEWriter()
		reqCtx, cancelReq := context.WithCancel(context.Background())
		defer cancelReq()
		done := make(chan error, 1)
		go func() { done <- s.Serve(reqCtx, writer) }()
		require.NoError(t, <-done)
		assert.Equal(t, "hard timeout", s.CloseReason())
		assert.Contains(t, writer.String(), `"reason":"hard timeout"`)
	})
}

func TestSessionWriterFailure(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}

	s, err := NewSession(
		context.Background(),
		RelayCommand{},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		tc,
		pg,
		zap.NewNop(),
	)
	require.NoError(t, err)

	writer := newTestSSEWriter()
	writer.failAfter = 1 // fail on second write
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()

	done := make(chan error, 1)
	go func() { done <- s.Serve(reqCtx, writer) }()

	require.Eventually(t, func() bool {
		return host.hasStream(s.StreamPID())
	}, time.Second, 10*time.Millisecond)

	// First write is detached "ready", this message triggers failed write.
	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		MessageTopic,
		payload.NewString("boom"),
	)))
	require.NoError(t, <-done)
	assert.Equal(t, "client disconnected", s.CloseReason())
}

func TestSessionBytesPayload(t *testing.T) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}

	s, err := NewSession(
		context.Background(),
		RelayCommand{},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		tc,
		pg,
		zap.NewNop(),
	)
	require.NoError(t, err)

	writer := newTestSSEWriter()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(reqCtx, writer) }()

	require.Eventually(t, func() bool {
		return host.hasStream(s.StreamPID())
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, host.Send(relay.NewPackage(
		pid.Zero(),
		s.StreamPID(),
		MessageTopic,
		payload.NewPayload([]byte{0x01, 0x02, 0x03}, payload.Bytes),
	)))
	require.Eventually(t, func() bool {
		return strings.Contains(writer.String(), "AQID")
	}, time.Second, 10*time.Millisecond)

	cancelReq()
	require.NoError(t, <-done)
}

type testPIDGen struct {
	n atomic.Uint64
}

func newManagedTestSession(
	t *testing.T,
	target pid.PID,
	host *mockHost,
	node *mockNode,
	topo *mockTopology,
	logger *zap.Logger,
) (*Session, error) {
	t.Helper()
	return NewSession(
		context.Background(),
		RelayCommand{TargetPID: target.String(), HeartbeatInterval: "0s"},
		registry.NewID("app", "server"),
		host,
		node,
		topo,
		&mockTranscoder{},
		&testPIDGen{},
		logger,
	)
}

func (g *testPIDGen) Generate(host pid.HostID) pid.PID {
	p := pid.PID{
		Node:   "n1",
		Host:   host,
		UniqID: fmt.Sprintf("sse-%d", g.n.Add(1)),
	}
	return p.Precomputed()
}

type mockHost struct {
	streams       map[string]chan *relay.Package
	mu            sync.RWMutex
	attachCancelN int
	detachN       int
}

func newMockHost() *mockHost {
	return &mockHost{streams: make(map[string]chan *relay.Package)}
}

func (h *mockHost) Attach(p pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.streams[p.String()] = ch
	return func() {
		h.mu.Lock()
		h.attachCancelN++
		h.mu.Unlock()
		h.Detach(p)
	}, nil
}

func (h *mockHost) Detach(p pid.PID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.detachN++
	delete(h.streams, p.String())
}

func (h *mockHost) Send(pkg *relay.Package) error {
	h.mu.RLock()
	ch := h.streams[pkg.Target.String()]
	h.mu.RUnlock()
	if ch == nil {
		return errors.New("target stream not attached")
	}

	select {
	case ch <- pkg:
		return nil
	case <-time.After(time.Second):
		return errors.New("send timeout")
	}
}

func (h *mockHost) hasStream(p pid.PID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.streams[p.String()]
	return ok
}

func (h *mockHost) cancelCalls() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.attachCancelN
}

func (h *mockHost) detachCalls() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.detachN
}

type mockNode struct {
	sendErrors map[string]error
	sendNotify map[string]chan struct{}
	sent       []*relay.Package
	mu         sync.Mutex
}

func newMockNode() *mockNode {
	return &mockNode{}
}

func (n *mockNode) ID() pid.NodeID {
	return "n1"
}

func (n *mockNode) RegisterHost(_ pid.HostID, _ relay.Receiver) error {
	return nil
}

func (n *mockNode) UnregisterHost(_ pid.HostID) {}

func (n *mockNode) GetHost(_ pid.HostID) (relay.Receiver, bool) {
	return nil, false
}

func (n *mockNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}

func (n *mockNode) Detach(_ pid.PID) {}

func (n *mockNode) Send(pkg *relay.Package) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, pkg)
	for _, msg := range pkg.Messages {
		key := nodeSendKey(msg.Topic, pkg.Target)
		if ch := n.sendNotify[key]; ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		if err := n.sendErrors[key]; err != nil {
			return err
		}
	}
	return nil
}

func (n *mockNode) notifyOnSend(topic string, target pid.PID) <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sendNotify == nil {
		n.sendNotify = make(map[string]chan struct{})
	}
	ch := make(chan struct{}, 1)
	n.sendNotify[nodeSendKey(topic, target)] = ch
	return ch
}

func (n *mockNode) setSendError(topic string, target pid.PID, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sendErrors == nil {
		n.sendErrors = make(map[string]error)
	}
	n.sendErrors[nodeSendKey(topic, target)] = err
}

func (n *mockNode) sendCount(topic string, target pid.PID) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, pkg := range n.sent {
		if pkg.Target.String() != target.String() {
			continue
		}
		for _, msg := range pkg.Messages {
			if msg.Topic == topic {
				count++
			}
		}
	}
	return count
}

func (n *mockNode) sendSource(topic string, target pid.PID) pid.PID {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := len(n.sent) - 1; i >= 0; i-- {
		pkg := n.sent[i]
		if pkg.Target.String() != target.String() {
			continue
		}
		for _, msg := range pkg.Messages {
			if msg.Topic == topic {
				return pkg.Source
			}
		}
	}
	return pid.Zero()
}

func (n *mockNode) hasTopic(topic string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.sent {
		for _, msg := range p.Messages {
			if msg.Topic == topic {
				return true
			}
		}
	}
	return false
}

func (n *mockNode) topicCount(topic string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, p := range n.sent {
		for _, msg := range p.Messages {
			if msg.Topic == topic {
				count++
			}
		}
	}
	return count
}

type monitorCall struct {
	caller pid.PID
	target pid.PID
}

type mockTopology struct {
	monitorErr     error
	demonitorErr   error
	registeredSet  map[string]bool
	completedSet   map[string]bool
	completeCalls  map[string]int
	monitorCalls   []monitorCall
	demonitorCalls []monitorCall
	mu             sync.Mutex
}

func newMockTopology() *mockTopology {
	return &mockTopology{
		registeredSet: make(map[string]bool),
		completedSet:  make(map[string]bool),
		completeCalls: make(map[string]int),
	}
}

func (t *mockTopology) Register(p pid.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.registeredSet[p.String()] = true
	return nil
}

func (t *mockTopology) Complete(p pid.PID, _ *runtime.Result) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.completedSet[p.String()] = true
	t.completeCalls[p.String()]++
}

func (t *mockTopology) Remove(_ pid.PID) {}

func (t *mockTopology) Monitor(caller, target pid.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.monitorErr != nil {
		return t.monitorErr
	}
	t.monitorCalls = append(t.monitorCalls, monitorCall{caller: caller, target: target})
	return nil
}

func (t *mockTopology) Demonitor(caller, target pid.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.demonitorCalls = append(t.demonitorCalls, monitorCall{caller: caller, target: target})
	if t.demonitorErr != nil {
		return t.demonitorErr
	}
	return nil
}

func (t *mockTopology) Link(_, _ pid.PID) error {
	return nil
}

func (t *mockTopology) Unlink(_, _ pid.PID) error {
	return nil
}

func (t *mockTopology) GetLinks(_ pid.PID) []pid.PID {
	return nil
}

func (t *mockTopology) registered(p pid.PID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.registeredSet[p.String()]
}

func (t *mockTopology) completed(p pid.PID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.completedSet[p.String()]
}

func (t *mockTopology) completeCount(p pid.PID) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.completeCalls[p.String()]
}

func (t *mockTopology) monitoredBy(caller, target pid.PID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.monitorCalls {
		if c.caller.String() == caller.String() && c.target.String() == target.String() {
			return true
		}
	}
	return false
}

func (t *mockTopology) demonitorCalled(caller, target pid.PID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.demonitorCalls {
		if c.caller.String() == caller.String() && c.target.String() == target.String() {
			return true
		}
	}
	return false
}

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	b, err := m.toJSON(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (m *mockTranscoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	if to != payload.JSON {
		return nil, fmt.Errorf("unsupported target format: %s", to)
	}
	b, err := m.toJSON(p)
	if err != nil {
		return nil, err
	}
	return payload.NewPayload(b, payload.JSON), nil
}

func (m *mockTranscoder) toJSON(p payload.Payload) ([]byte, error) {
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
		return json.Marshal(p.Data())
	}
}

type testSSEWriter struct {
	header       http.Header
	writeStarted chan struct{}
	writeRelease chan struct{}
	body         strings.Builder
	status       int
	flushed      int
	writeN       int
	failAfter    int
	failNext     bool
	mu           sync.Mutex
}

func newTestSSEWriter() *testSSEWriter {
	return &testSSEWriter{
		header: make(http.Header),
	}
}

func (w *testSSEWriter) Header() http.Header {
	return w.header
}

func (w *testSSEWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	started := w.writeStarted
	release := w.writeRelease
	failNext := w.failNext
	if started != nil {
		w.writeStarted = nil
		w.writeRelease = nil
		w.failNext = false
		w.mu.Unlock()
		started <- struct{}{}
		<-release
		w.mu.Lock()
		defer w.mu.Unlock()
		if failNext {
			return 0, errors.New("write failed")
		}
	} else {
		defer w.mu.Unlock()
	}
	if w.failAfter > 0 && w.writeN >= w.failAfter {
		return 0, errors.New("write failed")
	}
	w.writeN++
	return w.body.Write(p)
}

func (w *testSSEWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = statusCode
}

func (w *testSSEWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushed++
}

func (w *testSSEWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func (w *testSSEWriter) gateNextWrite(started chan struct{}, release chan struct{}, fail bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeStarted = started
	w.writeRelease = release
	w.failNext = fail
}

func assertSessionCleaned(t *testing.T, s *Session, host *mockHost, topo *mockTopology) {
	t.Helper()
	assert.False(t, host.hasStream(s.StreamPID()))
	assert.Equal(t, 1, topo.completeCount(s.StreamPID()))
	assert.False(t, s.hasTarget)
	assert.False(t, s.monitored)
	assert.False(t, s.joined)
	assert.True(t, s.targetPID.Equal(pid.Zero()))
	assert.Nil(t, s.heartbeatTicker)
	assert.Nil(t, s.idleTimer)
	assert.Nil(t, s.hardTimer)
	select {
	case <-s.ctx.Done():
	default:
		t.Fatal("session context was not canceled")
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func waitSessionDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("session did not finish")
	}
}

func nodeSendKey(topic string, target pid.PID) string {
	return topic + "\x00" + target.String()
}

func mustPID(raw string) pid.PID {
	p, err := pid.ParsePID(raw)
	if err != nil {
		panic(err)
	}
	return p
}
