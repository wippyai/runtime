// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"
	"fmt"
	"strconv"

	"connectrpc.com/connect"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
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
	ErrQuotaExceeded     = errors.New("quota exceeded")
	ErrHubUnavailable    = errors.New("hub unavailable")
)

type UnavailableError struct {
	cause      error
	RetryAfter string
	Detail     string
}

func (e *UnavailableError) Error() string {
	if seconds, err := strconv.Atoi(e.RetryAfter); err == nil && seconds >= 0 {
		return "hub rate limited the request, retry in " + e.RetryAfter + "s"
	}

	if e.RetryAfter != "" {
		return "hub rate limited the request (Retry-After: " + e.RetryAfter + ")"
	}

	if e.Detail != "" {
		return "hub unavailable: " + e.Detail
	}

	return "hub unavailable: network error or service overloaded"
}

func (e *UnavailableError) Is(target error) bool {
	return target == ErrHubUnavailable
}

func (e *UnavailableError) Unwrap() error {
	return e.cause
}

type QuotaExceededError struct {
	Reason string
}

func (e *QuotaExceededError) Error() string {
	if e.Reason == "" {
		return ErrQuotaExceeded.Error()
	}

	return ErrQuotaExceeded.Error() + ": " + e.Reason
}

func (e *QuotaExceededError) Is(target error) bool {
	return target == ErrQuotaExceeded
}

func QuotaReason(err error) string {
	var qe *QuotaExceededError
	if errors.As(err, &qe) {
		return qe.Reason
	}

	return ""
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
	case connect.CodeResourceExhausted:
		return &QuotaExceededError{Reason: connectErr.Message()}
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
	case connect.CodeUnavailable:
		return &UnavailableError{
			RetryAfter: connectErr.Meta().Get("Retry-After"),
			Detail:     connectErr.Message(),
			cause:      err,
		}
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

func NewDependencyEntryInvalidError(entryID, detail, component string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":  entryID,
			"detail":    detail,
			"component": component,
		}))
}

func NewDependencyEntryDecodeError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "decode dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID})).
		WithCause(cause)
}

func NewDependencyEntryMissingError(entryID string) apierror.Error {
	return apierror.New(apierror.NotFound, "dependency entry not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID}))
}

func NewDependencyResolutionError(cause error) apierror.Error {
	err := apierror.New(apierror.Unavailable, "dependency resolution failed").
		WithRetryable(apierror.False).
		WithCause(cause)
	if cause != nil {
		err = err.WithDetails(attrs.NewBagFrom(map[string]any{"reason": cause.Error()}))
	}
	return err
}

// NewDependencyOfflineError reports unavailable verified dependency evidence.
func NewDependencyOfflineError(operation, module string) apierror.Error {
	details := map[string]any{
		"operation": operation,
		"hint":      "run an explicit wippy update/install while online, then retry startup",
	}
	if module != "" {
		details["module"] = module
	}
	return apierror.New(apierror.Invalid, "verified dependency evidence is unavailable during offline startup").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}

func NewDependencyResolutionErrors(errs []ResolutionError) apierror.Error {
	details := make([]map[string]any, 0, len(errs))
	unauthenticated := false
	for _, e := range errs {
		details = append(details, map[string]any{
			"module":     e.Org + "/" + e.Name,
			"constraint": e.Constraint,
			"message":    e.Message,
		})
		if errors.Is(e.Err, ErrNotAuthenticated) {
			unauthenticated = true
		}
	}

	summary := formatResolutionErrors(errs)
	bag := map[string]any{
		"count":   len(errs),
		"summary": summary,
		"errors":  details,
	}
	if unauthenticated {
		bag["hint"] = registryAuthHint
	}

	message := "dependency resolution failed"
	if summary != "" {
		message += ": " + summary
	}

	return apierror.New(apierror.Conflict, message).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(bag))
}

func NewDependencyDownloadError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "module download failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"module": module})).
		WithCause(cause)
}

func NewDependencyLoadError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "load module entries failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path})).
		WithCause(cause)
}

func NewDependencyIntegrityError(module string, cause error, expectedDigest string, expectedSize uint64) apierror.Error {
	details := map[string]any{"module": module}
	if expectedDigest != "" {
		details["expected_digest"] = expectedDigest
	}
	if expectedSize > 0 {
		details["expected_size"] = expectedSize
	}

	return apierror.New(apierror.Invalid, "downloaded module artifact failed integrity verification").
		WithDetails(attrs.NewBagFrom(details)).
		WithCause(cause).
		WithRetryable(apierror.False)
}

func NewDependencyPipelineError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "dependency pipeline failed").WithCause(cause)
}

func NewDependencyEntryConflictError(entryID, existingModule, desiredModule string) apierror.Error {
	msg := fmt.Sprintf("entry %q conflicts: owned by %q, wanted by %q", entryID, existingModule, desiredModule)
	return apierror.New(apierror.Conflict, msg).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":        entryID,
			"existing_module": existingModule,
			"desired_module":  desiredModule,
		})).
		WithRetryable(apierror.False)
}

func NewDependencyRootConflictError(component, existingEntryID, requestedEntryID string) apierror.Error {
	msg := fmt.Sprintf("dependency component %q is already installed as %q; update that dependency instead of creating %q", component, existingEntryID, requestedEntryID)
	return apierror.New(apierror.Conflict, msg).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"component":          component,
			"existing_entry_id":  existingEntryID,
			"requested_entry_id": requestedEntryID,
		})).
		WithRetryable(apierror.False)
}
