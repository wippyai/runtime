// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrTranscoderNotFound = apierror.New(apierror.Internal, "transcoder not found in context").WithRetryable(apierror.False)
	ErrEmptyKey           = apierror.New(apierror.Invalid, "empty key").WithRetryable(apierror.False)
	ErrEmptyNamespace     = apierror.New(apierror.Invalid, "empty namespace").WithRetryable(apierror.False)
	ErrEmptyEntryName     = apierror.New(apierror.Invalid, "empty entry name").WithRetryable(apierror.False)
	ErrEmptyPath          = apierror.New(apierror.Invalid, "empty path").WithRetryable(apierror.False)
	ErrNoMatchingEntries  = apierror.New(apierror.NotFound, "no matching entries found").WithRetryable(apierror.False)
	ErrNoValueAvailable   = apierror.New(apierror.NotFound, "no value available: no dependency parameter found and no default value specified").WithRetryable(apierror.False)
)

func NewInvalidNamespacePatternError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid namespace pattern").WithCause(cause)
}

func NewInvalidEntryPatternError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid entry pattern").WithCause(cause)
}

func NewInvalidEntryPatternFormatError(pattern string, reason string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid entry pattern '%s': %s", pattern, reason))
}

func NewLoadDirectoryError(dir string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to load directory %s", dir)).WithCause(cause)
}

func NewInvalidKeyError(key string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid key %s", key)).WithCause(cause)
}

func NewEntryNotFoundError(namespace, entryName string) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("no entry found for %s:%s", namespace, entryName))
}

func NewSetValueError(namespace, entryName, path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to set %s:%s at %s", namespace, entryName, path)).WithCause(cause)
}

func NewOverrideErrors(errs []error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("override errors: %v", errs))
}

func NewMissingSeparatorError(separator, expected string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("missing %s separator (expected %s)", separator, expected))
}

func NewMissingFieldError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("missing %s", field))
}

func NewDecodeRequirementError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to decode requirement %s", entryID)).WithCause(cause)
}

func NewDecodeDependencyError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to decode dependency %s", entryID)).WithCause(cause)
}

func NewInvalidDependencyComponentError(entryID, component string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid dependency component %s", component)).
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID, "component": component})).
		WithCause(cause)
}

func NewRequirementError(requirementName, namespace string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("requirement %s in namespace %s failed", requirementName, namespace)).WithCause(cause)
}

func NewNoTargetsError(requirementID string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid requirement %s: no targets defined in requirement definition", requirementID))
}

func NewRequirementTargetError(requirement, targetEntry, path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("requirement %s, target entry=%s path=%s failed", requirement, targetEntry, path)).WithCause(cause)
}

func NewUnresolvedRequirementsError(errs []error) apierror.Error {
	details := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			details = append(details, err.Error())
		}
	}
	msg := "unresolved requirements"
	if len(details) > 0 {
		msg = fmt.Sprintf("%s: %s", msg, strings.Join(details, "; "))
	}
	return apierror.New(apierror.Invalid, msg).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"count":  len(details),
			"errors": details,
		})).
		WithRetryable(apierror.False)
}

func NewParameterConflictError(conflicts string) apierror.Error {
	return apierror.New(apierror.Conflict, fmt.Sprintf("parameter conflict: multiple dependencies define different values: %s", conflicts))
}

func NewAppendToEntryError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to append to entry %s", entryID)).WithCause(cause)
}

func NewSetValueInEntryError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to set value in entry %s", entryID)).WithCause(cause)
}

var (
	ErrBytecodeNoData          = apierror.New(apierror.Invalid, "entry has no data").WithRetryable(apierror.False)
	ErrBytecodeInvalidData     = apierror.New(apierror.Invalid, "entry data is not a map").WithRetryable(apierror.False)
	ErrBytecodeNoSource        = apierror.New(apierror.Invalid, "entry has no source field").WithRetryable(apierror.False)
	ErrBytecodeUnsupportedKind = apierror.New(apierror.Invalid, "unsupported entry kind for bytecode compilation").WithRetryable(apierror.False)

	ErrNoDefinition = apierror.New(apierror.Invalid, "ns.definition entry is required for publishing").WithRetryable(apierror.False)
)

func NewMultipleDefinitionsError(count int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("exactly one ns.definition entry required, found %d", count)).WithRetryable(apierror.False)
}

func NewBytecodeCompileError(entryID fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to compile bytecode for %s", entryID)).WithCause(cause)
}

func NewBytecodeTransformError(entryID fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to transform entry %s to bytecode config", entryID)).WithCause(cause)
}

func NewBytecodeParseError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse Lua source").WithCause(cause)
}

func NewBytecodeCompileLuaError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to compile Lua source").WithCause(cause)
}

func NewBytecodeDumpError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to dump bytecode").WithCause(cause)
}

func NewBytecodeTranscodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to transcode entry data").WithCause(cause)
}
