package di

import (
	"errors"
	"testing"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func getDetail(err *Error, key string) any {
	val, _ := err.Details().Get(key)
	return val
}

func TestError_Interface(t *testing.T) {
	err := &Error{
		kind:    apierror.KindInvalid,
		message: "test message",
	}

	var _ error = err
	if err.Error() != "test message" {
		t.Errorf("expected 'test message', got '%s'", err.Error())
	}
}

func TestError_Kind(t *testing.T) {
	err := &Error{kind: apierror.KindNotFound}
	if err.Kind() != apierror.KindNotFound {
		t.Errorf("expected KindNotFound, got %v", err.Kind())
	}
}

func TestError_Retryable(t *testing.T) {
	err := &Error{retryable: apierror.True}
	if err.Retryable() != apierror.True {
		t.Errorf("expected True, got %v", err.Retryable())
	}
}

func TestError_Details(t *testing.T) {
	err := &Error{}
	if err.Details() != nil {
		t.Errorf("expected nil details")
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("cause")
	err := &Error{cause: cause}
	if err.Unwrap() != cause {
		t.Errorf("expected cause error")
	}
}

func TestNewUnsupportedEntryKindError(t *testing.T) {
	err := NewUnsupportedEntryKindError("unknown.kind")
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if diErr.message != "unsupported entry kind: unknown.kind" {
		t.Errorf("unexpected message: %s", diErr.message)
	}
	if getDetail(diErr, "kind") != "unknown.kind" {
		t.Errorf("expected kind in details")
	}
}

func TestNewMethodNameEmptyError(t *testing.T) {
	defID := registry.ParseID("test/definition")
	err := NewMethodNameEmptyError(defID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "definition_id") != defID.String() {
		t.Errorf("expected definition_id in details")
	}
}

func TestNewDuplicateMethodNameError(t *testing.T) {
	defID := registry.ParseID("test/definition")
	err := NewDuplicateMethodNameError("doSomething", defID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "method_name") != "doSomething" {
		t.Errorf("expected method_name in details")
	}
	if getDetail(diErr, "definition_id") != defID.String() {
		t.Errorf("expected definition_id in details")
	}
}

func TestNewInputSchemaMissingFormatError(t *testing.T) {
	defID := registry.ParseID("test/definition")
	err := NewInputSchemaMissingFormatError(0, "myMethod", defID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "schema_index") != 0 {
		t.Errorf("expected schema_index in details")
	}
	if getDetail(diErr, "method_name") != "myMethod" {
		t.Errorf("expected method_name in details")
	}
}

func TestNewOutputSchemaMissingFormatError(t *testing.T) {
	defID := registry.ParseID("test/definition")
	err := NewOutputSchemaMissingFormatError(1, "myMethod", defID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "schema_index") != 1 {
		t.Errorf("expected schema_index in details")
	}
}

func TestNewBindingNoContractsError(t *testing.T) {
	bindingID := registry.ParseID("test/binding")
	err := NewBindingNoContractsError(bindingID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "binding_id") != bindingID.String() {
		t.Errorf("expected binding_id in details")
	}
}

func TestNewContractNotFoundError(t *testing.T) {
	bindingID := registry.ParseID("test/binding")
	contractID := registry.ParseID("test/contract")
	err := NewContractNotFoundError(bindingID, 0, contractID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "binding_id") != bindingID.String() {
		t.Errorf("expected binding_id in details")
	}
	if getDetail(diErr, "contract_index") != 0 {
		t.Errorf("expected contract_index in details")
	}
	if getDetail(diErr, "contract_id") != contractID.String() {
		t.Errorf("expected contract_id in details")
	}
}

func TestNewMethodNotBoundError(t *testing.T) {
	bindingID := registry.ParseID("test/binding")
	contractID := registry.ParseID("test/contract")
	err := NewMethodNotBoundError(bindingID, contractID, "myMethod")
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "method_name") != "myMethod" {
		t.Errorf("expected method_name in details")
	}
}

func TestNewMethodNotDefinedError(t *testing.T) {
	bindingID := registry.ParseID("test/binding")
	contractID := registry.ParseID("test/contract")
	err := NewMethodNotDefinedError(bindingID, contractID, "unknownMethod")
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "method_name") != "unknownMethod" {
		t.Errorf("expected method_name in details")
	}
}

func TestNewDuplicateDefaultBindingError(t *testing.T) {
	contractID := registry.ParseID("test/contract")
	existingID := registry.ParseID("test/existing")
	newID := registry.ParseID("test/new")
	err := NewDuplicateDefaultBindingError(contractID, existingID, newID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindAlreadyExists {
		t.Errorf("expected KindAlreadyExists")
	}
	if getDetail(diErr, "contract_id") != contractID.String() {
		t.Errorf("expected contract_id in details")
	}
	if getDetail(diErr, "existing_binding_id") != existingID.String() {
		t.Errorf("expected existing_binding_id in details")
	}
	if getDetail(diErr, "new_binding_id") != newID.String() {
		t.Errorf("expected new_binding_id in details")
	}
}

func TestNewDecodeDefinitionError(t *testing.T) {
	id := registry.ParseID("test/definition")
	cause := errors.New("decode failed")
	err := NewDecodeDefinitionError(id, cause)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if diErr.Unwrap() != cause {
		t.Errorf("expected cause to be unwrapped")
	}
}

func TestNewDefinitionAlreadyExistsError(t *testing.T) {
	id := registry.ParseID("test/definition")
	err := NewDefinitionAlreadyExistsError(id)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindAlreadyExists {
		t.Errorf("expected KindAlreadyExists")
	}
}

func TestNewDecodeDefinitionUpdateError(t *testing.T) {
	id := registry.ParseID("test/definition")
	cause := errors.New("decode failed")
	err := NewDecodeDefinitionUpdateError(id, cause)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if diErr.Unwrap() != cause {
		t.Errorf("expected cause to be unwrapped")
	}
}

func TestNewDefinitionNotFoundForUpdateError(t *testing.T) {
	id := registry.ParseID("test/definition")
	err := NewDefinitionNotFoundForUpdateError(id)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindNotFound {
		t.Errorf("expected KindNotFound")
	}
}

func TestNewUpdateWouldInvalidateBindingError(t *testing.T) {
	defID := registry.ParseID("test/definition")
	bindingID := registry.ParseID("test/binding")
	cause := errors.New("validation failed")
	err := NewUpdateWouldInvalidateBindingError(defID, bindingID, cause)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if diErr.Unwrap() != cause {
		t.Errorf("expected cause to be unwrapped")
	}
}

func TestNewDefinitionNotFoundForDeleteError(t *testing.T) {
	id := registry.ParseID("test/definition")
	err := NewDefinitionNotFoundForDeleteError(id)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindNotFound {
		t.Errorf("expected KindNotFound")
	}
}

func TestNewDefinitionInUseError(t *testing.T) {
	defID := registry.ParseID("test/definition")
	bindingID := registry.ParseID("test/binding")
	err := NewDefinitionInUseError(defID, bindingID)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if getDetail(diErr, "definition_id") != defID.String() {
		t.Errorf("expected definition_id in details")
	}
	if getDetail(diErr, "binding_id") != bindingID.String() {
		t.Errorf("expected binding_id in details")
	}
}

func TestNewDecodeBindingError(t *testing.T) {
	id := registry.ParseID("test/binding")
	cause := errors.New("decode failed")
	err := NewDecodeBindingError(id, cause)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if diErr.Unwrap() != cause {
		t.Errorf("expected cause to be unwrapped")
	}
}

func TestNewBindingAlreadyExistsError(t *testing.T) {
	id := registry.ParseID("test/binding")
	err := NewBindingAlreadyExistsError(id)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindAlreadyExists {
		t.Errorf("expected KindAlreadyExists")
	}
}

func TestNewDecodeBindingUpdateError(t *testing.T) {
	id := registry.ParseID("test/binding")
	cause := errors.New("decode failed")
	err := NewDecodeBindingUpdateError(id, cause)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindInvalid {
		t.Errorf("expected KindInvalid")
	}
	if diErr.Unwrap() != cause {
		t.Errorf("expected cause to be unwrapped")
	}
}

func TestNewBindingNotFoundForUpdateError(t *testing.T) {
	id := registry.ParseID("test/binding")
	err := NewBindingNotFoundForUpdateError(id)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindNotFound {
		t.Errorf("expected KindNotFound")
	}
}

func TestNewBindingNotFoundForDeleteError(t *testing.T) {
	id := registry.ParseID("test/binding")
	err := NewBindingNotFoundForDeleteError(id)
	diErr := err.(*Error)

	if diErr.Kind() != apierror.KindNotFound {
		t.Errorf("expected KindNotFound")
	}
}
