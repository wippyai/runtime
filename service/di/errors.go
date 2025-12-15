package di

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func NewUnsupportedEntryKindError(kind string) error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewMethodNameEmptyError(defID registry.ID) error {
	return apierror.New(apierror.Invalid, "method name cannot be empty in definition '"+defID.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"definition_id": defID.String()}))
}

func NewDuplicateMethodNameError(methodName string, defID registry.ID) error {
	return apierror.New(apierror.Invalid, "duplicate method name '"+methodName+"' in definition '"+defID.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"method_name":   methodName,
			"definition_id": defID.String(),
		}))
}

func NewInputSchemaMissingFormatError(index int, methodName string, defID registry.ID) error {
	return apierror.New(apierror.Invalid, "input schema has a definition but no format specified").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"schema_index":  index,
			"method_name":   methodName,
			"definition_id": defID.String(),
		}))
}

func NewOutputSchemaMissingFormatError(index int, methodName string, defID registry.ID) error {
	return apierror.New(apierror.Invalid, "output schema has a definition but no format specified").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"schema_index":  index,
			"method_name":   methodName,
			"definition_id": defID.String(),
		}))
}

func NewBindingNoContractsError(bindingID registry.ID) error {
	return apierror.New(apierror.Invalid, "binding '"+bindingID.String()+"' must bind at least one contract").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"binding_id": bindingID.String()}))
}

func NewContractNotFoundError(bindingID registry.ID, index int, contractID registry.ID) error {
	return apierror.New(apierror.Invalid, "binding '"+bindingID.String()+"' references undefined contract '"+contractID.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"binding_id":     bindingID.String(),
			"contract_index": index,
			"contract_id":    contractID.String(),
		}))
}

func NewMethodNotBoundError(bindingID, contractID registry.ID, methodName string) error {
	return apierror.New(apierror.Invalid, "binding '"+bindingID.String()+"' for contract '"+contractID.String()+"': method '"+methodName+"' defined in contract is not bound").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"binding_id":  bindingID.String(),
			"contract_id": contractID.String(),
			"method_name": methodName,
		}))
}

func NewMethodNotDefinedError(bindingID, contractID registry.ID, methodName string) error {
	return apierror.New(apierror.Invalid, "binding '"+bindingID.String()+"' for contract '"+contractID.String()+"': bound method '"+methodName+"' is not defined in contract definition").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"binding_id":  bindingID.String(),
			"contract_id": contractID.String(),
			"method_name": methodName,
		}))
}

func NewDuplicateDefaultBindingError(contractID, existingBindingID, newBindingID registry.ID) error {
	return apierror.New(apierror.AlreadyExists, "contract '"+contractID.String()+"' already has default binding '"+existingBindingID.String()+"', cannot set binding '"+newBindingID.String()+"' as default").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"contract_id":         contractID.String(),
			"existing_binding_id": existingBindingID.String(),
			"new_binding_id":      newBindingID.String(),
		}))
}

func NewDecodeDefinitionError(id registry.ID, cause error) error {
	return apierror.New(apierror.Invalid, "failed to decode definition '"+id.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"definition_id": id.String()})).
		WithCause(cause)
}

func NewDefinitionAlreadyExistsError(id registry.ID) error {
	return apierror.New(apierror.AlreadyExists, "contract definition '"+id.String()+"' already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"definition_id": id.String()}))
}

func NewDecodeDefinitionUpdateError(id registry.ID, cause error) error {
	return apierror.New(apierror.Invalid, "failed to decode definition for update '"+id.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"definition_id": id.String()})).
		WithCause(cause)
}

func NewDefinitionNotFoundForUpdateError(id registry.ID) error {
	return apierror.New(apierror.NotFound, "contract definition '"+id.String()+"' not found for update").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"definition_id": id.String()}))
}

func NewUpdateWouldInvalidateBindingError(defID, bindingID registry.ID, cause error) error {
	return apierror.New(apierror.Invalid, "updating definition '"+defID.String()+"' would invalidate binding '"+bindingID.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"definition_id": defID.String(),
			"binding_id":    bindingID.String(),
		})).
		WithCause(cause)
}

func NewDefinitionNotFoundForDeleteError(id registry.ID) error {
	return apierror.New(apierror.NotFound, "contract definition '"+id.String()+"' not found for deletion").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"definition_id": id.String()}))
}

func NewDefinitionInUseError(defID, bindingID registry.ID) error {
	return apierror.New(apierror.Invalid, "cannot delete contract definition '"+defID.String()+"': it is used by binding '"+bindingID.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"definition_id": defID.String(),
			"binding_id":    bindingID.String(),
		}))
}

func NewDecodeBindingError(id registry.ID, cause error) error {
	return apierror.New(apierror.Invalid, "failed to decode binding '"+id.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"binding_id": id.String()})).
		WithCause(cause)
}

func NewBindingAlreadyExistsError(id registry.ID) error {
	return apierror.New(apierror.AlreadyExists, "contract binding '"+id.String()+"' already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"binding_id": id.String()}))
}

func NewDecodeBindingUpdateError(id registry.ID, cause error) error {
	return apierror.New(apierror.Invalid, "failed to decode binding for update '"+id.String()+"'").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"binding_id": id.String()})).
		WithCause(cause)
}

func NewBindingNotFoundForUpdateError(id registry.ID) error {
	return apierror.New(apierror.NotFound, "contract binding '"+id.String()+"' not found for update").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"binding_id": id.String()}))
}

func NewBindingNotFoundForDeleteError(id registry.ID) error {
	return apierror.New(apierror.NotFound, "contract binding '"+id.String()+"' not found for deletion").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"binding_id": id.String()}))
}
