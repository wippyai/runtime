// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewStorageAlreadyExistsError(id string) error {
	return apierror.New(apierror.AlreadyExists, "storage already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewAddEntryError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to add entry").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewStorageNotFoundError(id string) error {
	return apierror.New(apierror.NotFound, "storage not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewUpdateEntryError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to update entry").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewDecodeConfigError(cause error) error {
	apiErr := apierror.New(apierror.Invalid, "failed to decode config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewAcquireResourceError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to acquire resource").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewGetConfigError(cause error) error {
	apiErr := apierror.New(apierror.Internal, "failed to get config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(cause)
	}
	return apiErr
}

func NewAWSConfigInvalidError() error {
	return apierror.New(apierror.Internal, "aws config resource is not a valid config").WithRetryable(apierror.False)
}

// NewInvalidRequestError reports a request the storage rejects before it
// reaches the provider.
func NewInvalidRequestError(message string) error {
	return apierror.New(apierror.Invalid, message).WithRetryable(apierror.False)
}

// errorClass is the runtime classification of a provider failure. sentinel is
// the cloudstorage sentinel the failure also satisfies through errors.Is.
type errorClass struct {
	sentinel error
	kind     apierror.Kind
}

var (
	classNotFound     = errorClass{kind: apierror.NotFound, sentinel: cloudstorage.ErrNotFound}
	classPrecondition = errorClass{kind: apierror.Conflict, sentinel: cloudstorage.ErrPreconditionFailed}
)

// errorCodeClasses classifies S3 error codes whose meaning is independent of,
// or more precise than, the HTTP status they arrive with.
var errorCodeClasses = map[string]errorClass{
	"NoSuchKey":               classNotFound,
	"NoSuchBucket":            classNotFound,
	"NoSuchUpload":            classNotFound,
	"NoSuchVersion":           classNotFound,
	"NotFound":                classNotFound,
	"PreconditionFailed":      classPrecondition,
	"NotModified":             classPrecondition,
	"AccessDenied":            {kind: apierror.PermissionDenied},
	"AllAccessDisabled":       {kind: apierror.PermissionDenied},
	"AccountProblem":          {kind: apierror.PermissionDenied},
	"InvalidAccessKeyId":      {kind: apierror.PermissionDenied},
	"InvalidSecurity":         {kind: apierror.PermissionDenied},
	"SignatureDoesNotMatch":   {kind: apierror.PermissionDenied},
	"ExpiredToken":            {kind: apierror.PermissionDenied},
	"InvalidToken":            {kind: apierror.PermissionDenied},
	"TokenRefreshRequired":    {kind: apierror.PermissionDenied},
	"BucketAlreadyExists":     {kind: apierror.Conflict},
	"BucketAlreadyOwnedByYou": {kind: apierror.Conflict},
	"OperationAborted":        {kind: apierror.Conflict},
	"InternalError":           {kind: apierror.Unavailable},
	"ServiceUnavailable":      {kind: apierror.Unavailable},
	"RequestTimeout":          {kind: apierror.Timeout},
}

// classifyErrorCode classifies an S3 error code. Throttling codes are the ones
// the AWS SDK retryer treats as throttles.
func classifyErrorCode(code string) (errorClass, bool) {
	if class, ok := errorCodeClasses[code]; ok {
		return class, true
	}
	if _, ok := retry.DefaultThrottleErrorCodes[code]; ok {
		return errorClass{kind: apierror.RateLimited}, true
	}
	return errorClass{}, false
}

func classifyHTTPStatus(status int) errorClass {
	switch {
	case status == http.StatusNotFound:
		return classNotFound
	case status == http.StatusPreconditionFailed, status == http.StatusNotModified:
		return classPrecondition
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return errorClass{kind: apierror.PermissionDenied}
	case status == http.StatusRequestTimeout:
		return errorClass{kind: apierror.Timeout}
	case status == http.StatusConflict:
		return errorClass{kind: apierror.Conflict}
	case status == http.StatusTooManyRequests:
		return errorClass{kind: apierror.RateLimited}
	case status >= http.StatusInternalServerError:
		return errorClass{kind: apierror.Unavailable}
	case status >= http.StatusBadRequest:
		return errorClass{kind: apierror.Invalid}
	default:
		return errorClass{kind: apierror.Internal}
	}
}

// transientKind reports whether a failure of this kind may succeed when the
// same request is repeated later.
func transientKind(kind apierror.Kind) bool {
	switch kind {
	case apierror.Unavailable, apierror.RateLimited, apierror.Timeout:
		return true
	default:
		return false
	}
}

var sdkRetryables = retry.IsErrorRetryables(retry.DefaultRetryables)

// classifyError classifies a failed provider call. The caller context decides
// cancellation and deadline expiry; otherwise a kind declared by the cause, the
// S3 error code, the HTTP status and finally the transport failure decide, in
// that order. Retryability follows the AWS SDK retryer verdict when it has one
// and the transience of the kind when it does not.
func classifyError(ctx context.Context, err error) (errorClass, apierror.Ternary) {
	switch ctxErr := ctx.Err(); {
	case errors.Is(ctxErr, context.Canceled):
		return errorClass{kind: apierror.Canceled}, apierror.False
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return errorClass{kind: apierror.Timeout}, apierror.False
	case errors.Is(err, context.Canceled):
		return errorClass{kind: apierror.Canceled}, apierror.False
	}

	var declared apierror.Error
	if errors.As(err, &declared) && declared.Kind() != "" && declared.Kind() != apierror.Unknown {
		return errorClass{kind: declared.Kind()}, declared.Retryable()
	}

	class := errorClass{kind: apierror.Internal}
	var apiErr smithy.APIError
	var netErr net.Error
	codeClass, known := errorClass{}, false
	if errors.As(err, &apiErr) {
		codeClass, known = classifyErrorCode(apiErr.ErrorCode())
	}
	status := responseStatus(err)
	switch {
	case known:
		class = codeClass
	case status > 0:
		class = classifyHTTPStatus(status)
	case errors.As(err, &netErr):
		class.kind = apierror.Unavailable
		if netErr.Timeout() {
			class.kind = apierror.Timeout
		}
	}

	switch sdkRetryables.IsErrorRetryable(err) {
	case aws.TrueTernary:
		return class, apierror.True
	case aws.FalseTernary:
		return class, apierror.False
	}
	if transientKind(class.kind) {
		return class, apierror.True
	}
	return class, apierror.False
}

// responseStatus returns the HTTP status of the provider response, or zero
// when the request failed before a response arrived.
func responseStatus(err error) int {
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		return status.HTTPStatusCode()
	}
	return 0
}

// storageError converts a failed provider call into a classified runtime error
// that keeps the provider error as its cause.
func storageError(ctx context.Context, bucket, action string, err error) error {
	if err == nil {
		return nil
	}
	class, retryable := classifyError(ctx, err)

	details := attrs.Bag{"bucket": bucket}
	var opErr *smithy.OperationError
	if errors.As(err, &opErr) {
		details["operation"] = opErr.Operation()
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() != "" {
		details["code"] = apiErr.ErrorCode()
	}
	if status := responseStatus(err); status > 0 {
		details["status_code"] = status
	}
	var reqErr interface{ ServiceRequestID() string }
	if errors.As(err, &reqErr) && reqErr.ServiceRequestID() != "" {
		details["request_id"] = reqErr.ServiceRequestID()
	}

	cause := err
	if class.sentinel != nil {
		cause = fmt.Errorf("%w: %w", class.sentinel, err)
	}
	return apierror.E(class.kind, action, retryable, details, cause)
}

// deleteObjectsError reports the per-key failures of a DeleteObjects call that
// the provider accepted. The failures share a kind when every code classifies
// to the same one; mixed failures are Internal.
func deleteObjectsError(bucket string, failures []types.Error) error {
	var message strings.Builder
	message.WriteString("delete objects failed: ")
	kind := apierror.Kind("")
	for i, failure := range failures {
		if i > 0 {
			message.WriteString("; ")
		}
		code := aws.ToString(failure.Code)
		message.WriteString("key \"")
		message.WriteString(aws.ToString(failure.Key))
		message.WriteString("\" [")
		message.WriteString(code)
		message.WriteByte(']')

		class, known := classifyErrorCode(code)
		if !known {
			class.kind = apierror.Internal
		}
		switch kind {
		case "":
			kind = class.kind
		case class.kind:
		default:
			kind = apierror.Internal
		}
	}

	details := attrs.Bag{
		"bucket":    bucket,
		"operation": "DeleteObjects",
		"failed":    len(failures),
	}
	if first := aws.ToString(failures[0].Code); allCodes(failures, first) {
		details["code"] = first
	}
	retryable := apierror.False
	if transientKind(kind) {
		retryable = apierror.True
	}
	return apierror.New(kind, message.String()).WithRetryable(retryable).WithDetails(details)
}

func allCodes(failures []types.Error, code string) bool {
	for _, failure := range failures {
		if aws.ToString(failure.Code) != code {
			return false
		}
	}
	return true
}
