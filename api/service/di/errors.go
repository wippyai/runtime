package di

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error represents a DI service error.
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

// NewUnsupportedEntryKindError creates an error for unsupported entry kinds.
func NewUnsupportedEntryKindError(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

// NewMethodNameEmptyError creates an error when method name is empty.
func NewMethodNameEmptyError(defID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "method name cannot be empty in definition '" + defID.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": defID.String()}),
	}
}

// NewDuplicateMethodNameError creates an error for duplicate method names.
func NewDuplicateMethodNameError(methodName string, defID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "duplicate method name '" + methodName + "' in definition '" + defID.String() + "'",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"method_name":   methodName,
			"definition_id": defID.String(),
		}),
	}
}

// NewInputSchemaMissingFormatError creates an error when input schema has no format.
func NewInputSchemaMissingFormatError(index int, methodName string, defID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "input schema has a definition but no format specified",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"schema_index":  index,
			"method_name":   methodName,
			"definition_id": defID.String(),
		}),
	}
}

// NewOutputSchemaMissingFormatError creates an error when output schema has no format.
func NewOutputSchemaMissingFormatError(index int, methodName string, defID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "output schema has a definition but no format specified",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"schema_index":  index,
			"method_name":   methodName,
			"definition_id": defID.String(),
		}),
	}
}

// NewBindingNoContractsError creates an error when binding has no contracts.
func NewBindingNoContractsError(bindingID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "binding '" + bindingID.String() + "' must bind at least one contract",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": bindingID.String()}),
	}
}

// NewContractNotFoundError creates an error when contract is not found.
func NewContractNotFoundError(bindingID registry.ID, index int, contractID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "binding '" + bindingID.String() + "' references undefined contract '" + contractID.String() + "'",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"binding_id":     bindingID.String(),
			"contract_index": index,
			"contract_id":    contractID.String(),
		}),
	}
}

// NewMethodNotBoundError creates an error when method is not bound.
func NewMethodNotBoundError(bindingID, contractID registry.ID, methodName string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "binding '" + bindingID.String() + "' for contract '" + contractID.String() + "': method '" + methodName + "' defined in contract is not bound",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"binding_id":  bindingID.String(),
			"contract_id": contractID.String(),
			"method_name": methodName,
		}),
	}
}

// NewMethodNotDefinedError creates an error when method is not defined.
func NewMethodNotDefinedError(bindingID, contractID registry.ID, methodName string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "binding '" + bindingID.String() + "' for contract '" + contractID.String() + "': bound method '" + methodName + "' is not defined in contract definition",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"binding_id":  bindingID.String(),
			"contract_id": contractID.String(),
			"method_name": methodName,
		}),
	}
}

// NewDuplicateDefaultBindingError creates an error when duplicate default binding.
func NewDuplicateDefaultBindingError(contractID, existingBindingID, newBindingID registry.ID) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "contract '" + contractID.String() + "' already has default binding '" + existingBindingID.String() + "', cannot set binding '" + newBindingID.String() + "' as default",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"contract_id":         contractID.String(),
			"existing_binding_id": existingBindingID.String(),
			"new_binding_id":      newBindingID.String(),
		}),
	}
}

// NewDecodeDefinitionError creates an error for definition decode failures.
func NewDecodeDefinitionError(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode definition '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
		cause:     cause,
	}
}

// NewDefinitionAlreadyExistsError creates an error when definition already exists.
func NewDefinitionAlreadyExistsError(id registry.ID) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "contract definition '" + id.String() + "' already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
	}
}

// NewDecodeDefinitionUpdateError creates an error for definition update decode failures.
func NewDecodeDefinitionUpdateError(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode definition for update '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
		cause:     cause,
	}
}

// NewDefinitionNotFoundForUpdateError creates an error when definition not found for update.
func NewDefinitionNotFoundForUpdateError(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract definition '" + id.String() + "' not found for update",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
	}
}

// NewUpdateWouldInvalidateBindingError creates an error when update would invalidate binding.
func NewUpdateWouldInvalidateBindingError(defID, bindingID registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "updating definition '" + defID.String() + "' would invalidate binding '" + bindingID.String() + "'",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"definition_id": defID.String(),
			"binding_id":    bindingID.String(),
		}),
		cause: cause,
	}
}

// NewDefinitionNotFoundForDeleteError creates an error when definition not found for delete.
func NewDefinitionNotFoundForDeleteError(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract definition '" + id.String() + "' not found for deletion",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
	}
}

// NewDefinitionInUseError creates an error when definition is in use.
func NewDefinitionInUseError(defID, bindingID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot delete contract definition '" + defID.String() + "': it is used by binding '" + bindingID.String() + "'",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"definition_id": defID.String(),
			"binding_id":    bindingID.String(),
		}),
	}
}

// NewDecodeBindingError creates an error for binding decode failures.
func NewDecodeBindingError(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode binding '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
		cause:     cause,
	}
}

// NewBindingAlreadyExistsError creates an error when binding already exists.
func NewBindingAlreadyExistsError(id registry.ID) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "contract binding '" + id.String() + "' already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}

// NewDecodeBindingUpdateError creates an error for binding update decode failures.
func NewDecodeBindingUpdateError(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode binding for update '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
		cause:     cause,
	}
}

// NewBindingNotFoundForUpdateError creates an error when binding not found for update.
func NewBindingNotFoundForUpdateError(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract binding '" + id.String() + "' not found for update",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}

// NewBindingNotFoundForDeleteError creates an error when binding not found for delete.
func NewBindingNotFoundForDeleteError(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract binding '" + id.String() + "' not found for deletion",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}
