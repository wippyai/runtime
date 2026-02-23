// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"

	"connectrpc.com/connect"
)

var (
	ErrNotAuthenticated  = errors.New("not authenticated")
	ErrVersionExists     = errors.New("version already exists")
	ErrInvalidVersion    = errors.New("invalid version format")
	ErrOrgAccessDenied   = errors.New("organization access denied")
	ErrModuleNotFound    = errors.New("module not found")
	ErrDigestMismatch    = errors.New("digest mismatch")
	ErrUploadExpired     = errors.New("upload URL expired")
	ErrPublishInProgress = errors.New("publish already in progress")
)

func MapConnectError(err error) error {
	if err == nil {
		return nil
	}

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}

	switch connectErr.Code() {
	case connect.CodeUnauthenticated:
		return ErrNotAuthenticated
	case connect.CodePermissionDenied:
		return ErrOrgAccessDenied
	case connect.CodeNotFound:
		return ErrModuleNotFound
	case connect.CodeAlreadyExists:
		return ErrVersionExists
	case connect.CodeInvalidArgument:
		if containsMessage(connectErr, "version") {
			return ErrInvalidVersion
		}
		if containsMessage(connectErr, "digest") {
			return ErrDigestMismatch
		}
		return err
	case connect.CodeFailedPrecondition:
		if containsMessage(connectErr, "expired") {
			return ErrUploadExpired
		}
		if containsMessage(connectErr, "progress") {
			return ErrPublishInProgress
		}
		return err
	default:
		return err
	}
}

func containsMessage(err *connect.Error, substr string) bool {
	return err != nil && err.Message() != "" && contains(err.Message(), substr)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
