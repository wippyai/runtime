package di

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

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

func errUnsupportedEntryKind(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func errMethodNameEmpty(defID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "method name cannot be empty in definition '" + defID.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": defID.String()}),
	}
}

func errDuplicateMethodName(methodName string, defID registry.ID) error {
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

func errInputSchemaMissingFormat(index int, methodName string, defID registry.ID) error {
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

func errOutputSchemaMissingFormat(index int, methodName string, defID registry.ID) error {
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

func errBindingNoContracts(bindingID registry.ID) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "binding '" + bindingID.String() + "' must bind at least one contract",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": bindingID.String()}),
	}
}

func errContractNotFound(bindingID registry.ID, index int, contractID registry.ID) error {
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

func errMethodNotBound(bindingID, contractID registry.ID, methodName string) error {
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

func errMethodNotDefined(bindingID, contractID registry.ID, methodName string) error {
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

func errDuplicateDefaultBinding(contractID, existingBindingID, newBindingID registry.ID) error {
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

func errDecodeDefinition(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode definition '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
		cause:     cause,
	}
}

func errDefinitionAlreadyExists(id registry.ID) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "contract definition '" + id.String() + "' already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
	}
}

func errDecodeDefinitionUpdate(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode definition for update '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
		cause:     cause,
	}
}

func errDefinitionNotFoundForUpdate(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract definition '" + id.String() + "' not found for update",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
	}
}

func errUpdateWouldInvalidateBinding(defID, bindingID registry.ID, cause error) error {
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

func errDefinitionNotFoundForDelete(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract definition '" + id.String() + "' not found for deletion",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"definition_id": id.String()}),
	}
}

func errDefinitionInUse(defID, bindingID registry.ID) error {
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

func errDecodeBinding(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode binding '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
		cause:     cause,
	}
}

func errBindingAlreadyExists(id registry.ID) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "contract binding '" + id.String() + "' already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}

func errDecodeBindingUpdate(id registry.ID, cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode binding for update '" + id.String() + "'",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
		cause:     cause,
	}
}

func errBindingNotFoundForUpdate(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract binding '" + id.String() + "' not found for update",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}

func errBindingNotFoundForDelete(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "contract binding '" + id.String() + "' not found for deletion",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"binding_id": id.String()}),
	}
}
