// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"connectrpc.com/connect"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Conditions the Hub reports, carrying the kind and retryability a caller needs
// to decide without parsing a message. Callers match them with errors.Is.
var (
	ErrNotAuthenticated  = apierror.New(apierror.PermissionDenied, "not authenticated").WithRetryable(apierror.False)
	ErrVersionExists     = apierror.New(apierror.AlreadyExists, "version already exists").WithRetryable(apierror.False)
	ErrInvalidVersion    = apierror.New(apierror.Invalid, "invalid version format").WithRetryable(apierror.False)
	ErrOrgAccessDenied   = apierror.New(apierror.PermissionDenied, "organization access denied").WithRetryable(apierror.False)
	ErrModuleNotFound    = apierror.New(apierror.NotFound, "module not found").WithRetryable(apierror.False)
	ErrDigestMismatch    = apierror.New(apierror.Invalid, "digest mismatch").WithRetryable(apierror.False)
	ErrUploadExpired     = apierror.New(apierror.Invalid, "upload URL expired").WithRetryable(apierror.False)
	ErrPublishInProgress = apierror.New(apierror.Conflict, "publish already in progress").WithRetryable(apierror.False)
	ErrQuotaExceeded     = apierror.New(apierror.RateLimited, "quota exceeded").WithRetryable(apierror.False)
	ErrHubUnavailable    = apierror.New(apierror.Unavailable, "hub unavailable").WithRetryable(apierror.True)
	// ErrModuleAlreadyExists reports a register request the Hub answered with 409.
	ErrModuleAlreadyExists = apierror.New(apierror.AlreadyExists, "module already exists").WithRetryable(apierror.False)

	// Causes a replacement fails verification with. They are sentinels so a
	// caller can match the reason rather than parse a message.
	errReplacementNotDirectory       = errors.New("replacement path is not a directory")
	errReplacementChangedWhileLoad   = errors.New("replacement changed while it was being loaded")
	errReplacementDigestMismatch     = errors.New("replacement content digest mismatch")
	errReplacementSizeMismatch       = errors.New("replacement content size mismatch")
	errStoredReplacementUnconfigured = errors.New("stored local replacement is not configured")
)

var (
	_ apierror.Error = (*UnavailableError)(nil)
	_ apierror.Error = (*QuotaExceededError)(nil)
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

// Kind, Retryable and Details put this error on the same surface as the rest of
// the package. The type stays because its message varies with Retry-After while
// it must still match ErrHubUnavailable, and apierror compares kind and message
// for equality, so a varying message could not match a fixed sentinel.
func (e *UnavailableError) Kind() apierror.Kind { return apierror.Unavailable }

func (e *UnavailableError) Retryable() apierror.Ternary { return apierror.True }

func (e *UnavailableError) Details() attrs.Attributes {
	details := map[string]any{}
	if e.RetryAfter != "" {
		details["retry_after"] = e.RetryAfter
	}
	if e.Detail != "" {
		details["detail"] = e.Detail
	}
	return attrs.NewBagFrom(details)
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

// Kind, Retryable and Details put this error on the same surface as the rest of
// the package, for the same reason as UnavailableError.
func (e *QuotaExceededError) Kind() apierror.Kind { return apierror.RateLimited }

func (e *QuotaExceededError) Retryable() apierror.Ternary { return apierror.False }

func (e *QuotaExceededError) Details() attrs.Attributes {
	details := map[string]any{}
	if e.Reason != "" {
		details["reason"] = e.Reason
	}
	return attrs.NewBagFrom(details)
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
	return apierror.New(apierror.Invalid, "invalid dependency entry: "+detail).
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

// withCauseDetails carries a rich cause's details onto the wrapping error.
// Nesting one apierror inside another otherwise hides the inner details from
// errors.As, which stops at the outermost match, so a caller inspecting the
// error it received would lose the module or path the cause identified.
func withCauseDetails(details map[string]any, cause error) map[string]any {
	var rich apierror.Error
	if !errors.As(cause, &rich) {
		return details
	}
	bag, ok := rich.Details().(attrs.Bag)
	if !ok {
		return details
	}
	bag.Iterate(func(key string, value any) {
		if _, taken := details[key]; !taken {
			details[key] = value
		}
	})
	return details
}

// NewVersionSelectionError reports a constraint the resolver cannot satisfy.
// The cause carries the sentinel a caller matches on with errors.Is.
func NewVersionSelectionError(detail string, cause error, fields map[string]any) apierror.Error {
	details := map[string]any{"detail": detail}
	for key, value := range fields {
		details[key] = value
	}
	return apierror.New(apierror.Invalid, detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(withCauseDetails(details, cause))).
		WithCause(cause)
}

// NewHubRequestError reports a failed exchange with the Hub service: building,
// sending, or decoding a request the caller may retry once connectivity holds.
func NewHubRequestError(operation string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "hub request failed: "+operation).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation})).
		WithCause(cause)
}

// NewHubResponseError reports a Hub response the client cannot act on.
func NewHubResponseError(operation string, status int, body string) apierror.Error {
	return apierror.New(apierror.Unavailable, fmt.Sprintf("hub returned an unusable response: %s %d", operation, status)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"operation": operation,
			"status":    status,
			"body":      strings.TrimSpace(body),
		}))
}

// NewArtifactIOError reports failed local filesystem work behind the artifact
// cache, staged module trees, and downloads.
func NewArtifactIOError(operation, target string, cause error) apierror.Error {
	details := map[string]any{"operation": operation}
	subject := operation
	if target != "" {
		details["target"] = target
		subject = operation + " " + target
	}
	return apierror.New(apierror.Internal, "artifact storage operation failed: "+subject).
		WithDetails(attrs.NewBagFrom(withCauseDetails(details, cause))).
		WithCause(cause)
}

// NewArtifactPathError reports a path that leaves, or cannot be confined to,
// the vendor directory it must stay inside.
func NewArtifactPathError(detail, path string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "artifact path is not usable: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "path": path})).
		WithCause(cause)
}

// NewArtifactContentError reports content that fails its recorded identity,
// where no module name attributes the failure.
func NewArtifactContentError(detail string, fields map[string]any) apierror.Error {
	details := map[string]any{"detail": detail}
	for key, value := range fields {
		details[key] = value
	}
	return apierror.New(apierror.Invalid, "artifact content failed verification: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}

// NewModuleIdentityError reports a module name, version or digest that does not
// satisfy the form the resolver and cache require.
func NewModuleIdentityError(detail, module string, fields map[string]any) apierror.Error {
	details := map[string]any{"detail": detail}
	if module != "" {
		details["module"] = module
	}
	for key, value := range fields {
		details[key] = value
	}
	return apierror.New(apierror.Invalid, "module identity is invalid: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}

// NewModuleTreeError reports a replacement tree holding an entry the digest
// cannot represent reproducibly.
func NewModuleTreeError(detail, path string) apierror.Error {
	return apierror.New(apierror.Invalid, "module tree is not reproducible: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail, "path": path}))
}

// NewStoredResolutionError reports persisted resolution state that no longer
// describes a graph the handler can rebuild.
func NewStoredResolutionError(detail string, fields map[string]any) apierror.Error {
	details := map[string]any{"detail": detail}
	for key, value := range fields {
		details[key] = value
	}
	message := "stored dependency resolution is invalid"
	if detail != "" {
		message += ": " + detail
	}
	return apierror.New(apierror.Invalid, message).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}

// NewEffectStateError reports an effect driven out of its lifecycle order.
func NewEffectStateError(operation string, state int) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("module effect reached an unexpected state: %s in state %d", operation, state)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation, "state": state}))
}

// NewClientConfigError reports Hub client options that cannot form a client.
func NewClientConfigError(detail string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "hub client configuration is invalid: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"detail": detail})).
		WithCause(cause)
}

// NewPublishError reports a publish the Hub did not complete.
func NewPublishError(detail string, fields map[string]any) apierror.Error {
	details := map[string]any{"detail": detail}
	for key, value := range fields {
		details[key] = value
	}
	return apierror.New(apierror.Internal, "module publish did not complete: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}

// NewRestoreReadError reports history the restore path could not read.
func NewRestoreReadError(operation string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "dependency restore could not read registry history: "+operation).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation})).
		WithCause(cause)
}

// NewDeploymentBaselineError reports a deployment baseline that cannot anchor a
// restore, naming what the operator must do to supply one.
func NewDeploymentBaselineError(detail, hint string) apierror.Error {
	details := map[string]any{"detail": detail}
	if hint != "" {
		details["hint"] = hint
	}
	return apierror.New(apierror.Invalid, "deployment baseline is unusable: "+detail).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
}
