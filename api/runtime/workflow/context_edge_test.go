package workflow

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- GetInfo ---

type testInfoProvider struct {
	info Info
}

func (p *testInfoProvider) GetWorkflowInfo() Info {
	return p.info
}

func TestGetInfo_NoFrame(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, GetInfo(ctx))
}

func TestGetInfo_NotSet(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	assert.Nil(t, GetInfo(ctx))
}

func TestGetInfo_WithProvider(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	provider := &testInfoProvider{
		info: Info{
			WorkflowID:   "wf-123",
			RunID:        "run-456",
			WorkflowType: "my-workflow",
			TaskQueue:    "default",
			Namespace:    "test-ns",
			Attempt:      1,
		},
	}

	require.NoError(t, SetInfoProvider(ctx, provider))

	info := GetInfo(ctx)
	require.NotNil(t, info)
	assert.Equal(t, "wf-123", info.WorkflowID)
	assert.Equal(t, "run-456", info.RunID)
	assert.Equal(t, "my-workflow", info.WorkflowType)
	assert.Equal(t, "default", info.TaskQueue)
	assert.Equal(t, "test-ns", info.Namespace)
	assert.Equal(t, 1, info.Attempt)
}

// --- SetInfoProvider ---

func TestSetInfoProvider_NoFrame(t *testing.T) {
	ctx := context.Background()
	err := SetInfoProvider(ctx, &testInfoProvider{})
	assert.Equal(t, ctxapi.ErrNoFrameContext, err)
}

func TestSetInfoProvider_SealedFrame(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	fc.Seal()

	err := SetInfoProvider(ctx, &testInfoProvider{})
	require.Error(t, err)
}

// --- Command types ---

func TestSideEffectCmd_CmdID(t *testing.T) {
	cmd := &SideEffectCmd{}
	assert.Equal(t, SideEffect, cmd.CmdID())
}

func TestExecCmd_CmdID(t *testing.T) {
	cmd := &ExecCmd{}
	assert.Equal(t, Exec, cmd.CmdID())
}

func TestVersionCmd_CmdID(t *testing.T) {
	cmd := &VersionCmd{ChangeID: "change-1", MinSupported: 1, MaxSupported: 3}
	assert.Equal(t, Version, cmd.CmdID())
}

func TestUpsertAttrsCmd_CmdID(t *testing.T) {
	cmd := &UpsertAttrsCmd{
		SearchAttrs: map[string]any{"key": "val"},
		Memo:        map[string]any{"memo": "data"},
	}
	assert.Equal(t, UpsertAttrs, cmd.CmdID())
}

// --- IsDeterministic edge case ---

func TestIsDeterministic_WrongValueType(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	// set a non-bool value under the deterministic key
	require.NoError(t, fc.Set(deterministicKey, "not-a-bool"))

	assert.False(t, IsDeterministic(ctx))
}

// --- GetInfo edge case ---

func TestGetInfo_WrongValueType(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	// set a non-InfoProvider value under the info key
	require.NoError(t, fc.Set(infoKey, "not-a-provider"))

	assert.Nil(t, GetInfo(ctx))
}
