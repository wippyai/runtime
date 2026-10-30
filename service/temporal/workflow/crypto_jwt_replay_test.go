// SPDX-License-Identifier: MPL-2.0

package workflow_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	securityapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	cryptomod "github.com/wippyai/runtime/runtime/lua/modules/crypto"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	wippyworkflow "github.com/wippyai/runtime/service/temporal/workflow"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestES256SigningReplaysRecordedToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	privateKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	publicKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	recordedAt := time.Unix(1700000000, 0)
	historicalToken, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"sub": "historical", "exp": recordedAt.Add(time.Hour).Unix()}).SignedString(key)
	require.NoError(t, err)
	id := registry.NewID("test.workflow", "jwt_replay")
	ctx := securityapi.SetStrictMode(ctxapi.NewRootContext(), false)
	transcoder := newTestTranscoder()
	payload.WithTranscoder(ctx, transcoder)
	dc := dataconverter.NewDataConverter(transcoder)
	manager, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{Modules: []*luaapi.ModuleDef{cryptomod.Module}})
	require.NoError(t, err)
	require.NoError(t, manager.AddNode(ctx, code.Node{
		ID: id, Kind: luaapi.Workflow, Method: "main", Source: `
local crypto = require("crypto")
return function(private_key, public_key, historical_token)
    local token, err = crypto.jwt.encode({sub="workflow"}, private_key, "ES256")
    assert(token and not err, tostring(err))
    assert(token == "recorded-token", "replay generated a new signature")
    local claims, verify_err = crypto.jwt.verify(historical_token, public_key, "ES256")
    assert(claims and not verify_err, tostring(verify_err))
    assert(claims.sub == "historical")
    return token
end`,
	}, []code.Import{{ID: registry.NewID("", "crypto"), Alias: "crypto"}}))
	factory, err := engine.NewProcessFactory(manager).CreateFactory(id, engine.WithAllowedClasses(
		luaapi.ClassDeterministic, luaapi.ClassWorkflow, luaapi.ClassSecurity, luaapi.ClassNondeterministic))
	require.NoError(t, err)
	ctx = process.WithFactory(ctx, replayProcessFactory{create: factory})
	input, err := dc.ToPayloads(payload.Payloads{payload.NewString(privateKey), payload.NewString(publicKey), payload.NewString(historicalToken)})
	require.NoError(t, err)
	markerID, err := dc.ToPayloads(int64(1))
	require.NoError(t, err)
	// A sentinel makes regeneration fail deterministically. The side-effect
	// result must come from history without invoking the random signer again.
	recorded, err := dc.ToPayloads("recorded-token")
	require.NoError(t, err)
	result, err := dc.ToPayloads(payload.Payloads{payload.NewString("recorded-token")})
	require.NoError(t, err)
	queue := &taskqueuepb.TaskQueue{Name: "jwt-replay"}
	history := &historypb.History{Events: []*historypb.HistoryEvent{
		{EventId: 1, EventTime: timestamppb.New(recordedAt), EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{
				WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{
					WorkflowType: &commonpb.WorkflowType{Name: id.String()}, TaskQueue: queue, Input: input,
				}}},
		{EventId: 2, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{
				WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{TaskQueue: queue}}},
		{EventId: 3, EventTime: timestamppb.New(recordedAt), EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{
				WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 2}}},
		{EventId: 4, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{
				WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 2, StartedEventId: 3}}},
		{EventId: 5, EventType: enumspb.EVENT_TYPE_MARKER_RECORDED,
			Attributes: &historypb.HistoryEvent_MarkerRecordedEventAttributes{
				MarkerRecordedEventAttributes: &historypb.MarkerRecordedEventAttributes{
					MarkerName: "SideEffect", WorkflowTaskCompletedEventId: 4,
					Details: map[string]*commonpb.Payloads{"side-effect-id": markerID, "data": recorded},
				}}},
		{EventId: 6, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{
				WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{
					WorkflowTaskCompletedEventId: 4, Result: result,
				}}},
	}}
	for range 2 {
		replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: dc})
		require.NoError(t, err)
		replayer.RegisterWorkflowWithOptions((&wippyworkflow.DefinitionFactory{ID: id}).WithContext(ctx), workflow.RegisterOptions{Name: id.String()})
		require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
	}
}
