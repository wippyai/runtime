package hub

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

func TestMapConnectError_Nil(t *testing.T) {
	assert.Nil(t, MapConnectError(nil))
}

func TestMapConnectError_NonConnectError(t *testing.T) {
	plain := errors.New("plain error")
	assert.Equal(t, plain, MapConnectError(plain))
}

func TestMapConnectError_Unauthenticated(t *testing.T) {
	err := connect.NewError(connect.CodeUnauthenticated, errors.New("bad token"))
	assert.Equal(t, ErrNotAuthenticated, MapConnectError(err))
}

func TestMapConnectError_PermissionDenied(t *testing.T) {
	err := connect.NewError(connect.CodePermissionDenied, errors.New("forbidden"))
	assert.Equal(t, ErrOrgAccessDenied, MapConnectError(err))
}

func TestMapConnectError_NotFound(t *testing.T) {
	err := connect.NewError(connect.CodeNotFound, errors.New("missing"))
	assert.Equal(t, ErrModuleNotFound, MapConnectError(err))
}

func TestMapConnectError_AlreadyExists(t *testing.T) {
	err := connect.NewError(connect.CodeAlreadyExists, errors.New("duplicate"))
	assert.Equal(t, ErrVersionExists, MapConnectError(err))
}

func TestMapConnectError_InvalidArgument_Version(t *testing.T) {
	err := connect.NewError(connect.CodeInvalidArgument, errors.New("invalid version format"))
	assert.Equal(t, ErrInvalidVersion, MapConnectError(err))
}

func TestMapConnectError_InvalidArgument_Digest(t *testing.T) {
	err := connect.NewError(connect.CodeInvalidArgument, errors.New("digest mismatch"))
	assert.Equal(t, ErrDigestMismatch, MapConnectError(err))
}

func TestMapConnectError_InvalidArgument_Other(t *testing.T) {
	err := connect.NewError(connect.CodeInvalidArgument, errors.New("something else"))
	result := MapConnectError(err)
	// returns original connect error
	assert.NotEqual(t, ErrInvalidVersion, result)
	assert.NotEqual(t, ErrDigestMismatch, result)
}

func TestMapConnectError_FailedPrecondition_Expired(t *testing.T) {
	err := connect.NewError(connect.CodeFailedPrecondition, errors.New("upload URL expired"))
	assert.Equal(t, ErrUploadExpired, MapConnectError(err))
}

func TestMapConnectError_FailedPrecondition_Progress(t *testing.T) {
	err := connect.NewError(connect.CodeFailedPrecondition, errors.New("publish in progress"))
	assert.Equal(t, ErrPublishInProgress, MapConnectError(err))
}

func TestMapConnectError_FailedPrecondition_Other(t *testing.T) {
	err := connect.NewError(connect.CodeFailedPrecondition, errors.New("something else"))
	result := MapConnectError(err)
	assert.NotEqual(t, ErrUploadExpired, result)
	assert.NotEqual(t, ErrPublishInProgress, result)
}

func TestMapConnectError_UnknownCode(t *testing.T) {
	err := connect.NewError(connect.CodeInternal, errors.New("server error"))
	result := MapConnectError(err)
	// returns original connect error
	var connectErr *connect.Error
	assert.True(t, errors.As(result, &connectErr))
}

func TestContainsMessage(t *testing.T) {
	tests := []struct {
		name   string
		err    *connect.Error
		substr string
		want   bool
	}{
		{"nil error", nil, "test", false},
		{"empty message", connect.NewError(connect.CodeInternal, errors.New("")), "test", false},
		{"match", connect.NewError(connect.CodeInternal, errors.New("invalid version")), "version", true},
		{"no match", connect.NewError(connect.CodeInternal, errors.New("something")), "version", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, containsMessage(tt.err, tt.substr))
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"hello world", "world", true},
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hi", "hello", false},
		{"", "", true},
		{"a", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.sub, func(t *testing.T) {
			assert.Equal(t, tt.want, contains(tt.s, tt.sub))
		})
	}
}

func TestSearchSubstring(t *testing.T) {
	assert.True(t, searchSubstring("abcdef", "cde"))
	assert.True(t, searchSubstring("abc", "abc"))
	assert.False(t, searchSubstring("abc", "xyz"))
	assert.True(t, searchSubstring("abc", ""))
}
