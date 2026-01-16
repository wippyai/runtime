package evalhost

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrNoResult = apierror.New(apierror.Internal, "process completed with no result").WithRetryable(apierror.False)

	ErrProcessIdle = apierror.New(apierror.Internal, "process became idle").WithRetryable(apierror.False)

	ErrYieldsNotSupported = apierror.New(apierror.Internal, "eval does not support async operations (http, sleep, etc)").WithRetryable(apierror.False)

	ErrYieldNotAllowed = apierror.New(apierror.Invalid, "yield command is not allowed in eval context").WithRetryable(apierror.False)

	ErrMaxStepsExceeded = apierror.New(apierror.Internal, "eval exceeded maximum step limit").WithRetryable(apierror.False)
)

func NewCompileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "compile failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateProcessError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create process").WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleNotAvailableError(name string) apierror.Error {
	return apierror.New(apierror.NotFound, "module \""+name+"\" is not available").WithRetryable(apierror.False)
}

func NewModuleNotAvailableErrorWithContext(name string, available []string) apierror.Error {
	msg := "module \"" + name + "\" is not available (have " + fmt.Sprintf("%d", len(available)) + " modules: " + strings.Join(available, ", ") + ")"
	return apierror.New(apierror.NotFound, msg).WithRetryable(apierror.False)
}

func NewParseError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "parse error").WithCause(cause).WithRetryable(apierror.False)
}

func NewCompileScriptError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "compile error").WithCause(cause).WithRetryable(apierror.False)
}

func NewForbiddenClassError(module, class string) apierror.Error {
	return apierror.New(apierror.Invalid, "module \""+module+"\" has forbidden class \""+class+"\"").WithRetryable(apierror.False)
}

func NewRunError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "run failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewNoHandlerError(cmdID dispatcher.CommandID) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("no handler for yield command %d", cmdID)).WithRetryable(apierror.False)
}

func NewImportError(alias string, id registry.ID, cause error) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("failed to import %q (%s)", alias, id.String())).WithCause(cause).WithRetryable(apierror.False)
}
