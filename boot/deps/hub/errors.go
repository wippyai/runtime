// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"
	"fmt"

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

// DecorateAuthError adds an actionable hint when an error is likely caused
// by missing or invalid credentials. The hub deliberately returns NotFound
// for private modules the caller can't see, so an unauthenticated NotFound
// is almost always a permission issue rather than a typo.
//
// hasToken should be true when the caller had a credential available — this
// distinguishes "you forgot to log in" from "your account doesn't have
// access".
func DecorateAuthError(err error, hasToken bool) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrModuleNotFound) && !hasToken:
		return fmt.Errorf("%w (if this module is private, run 'wippy auth login')", err)
	case errors.Is(err, ErrNotAuthenticated):
		return fmt.Errorf("%w (token missing or invalid; run 'wippy auth login')", err)
	case errors.Is(err, ErrOrgAccessDenied):
		return fmt.Errorf("%w (your account does not have access to this organization)", err)
	default:
		return err
	}
}

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
