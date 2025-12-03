package supervisor

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

// Helper function to create a no-op logger for testing
func noopLogger() *zap.Logger {
	return zap.NewNop()
}

func TestTransactionHelper_Begin(t *testing.T) {
	th := newTransactionHelper(noopLogger())

	// First begin should open the transaction
	th.begin()
	if !th.open {
		t.Error("transaction should be open after begin")
	}

	// Subsequent begin should reset the state
	assert.NoError(t, th.registerService("testService", &supervisor.Entry{}))
	assert.NoError(t, th.removeService("anotherService"))
	th.begin()

	if !th.open {
		t.Error("transaction should still be open after second begin")
	}
	if len(th.register) != 0 || len(th.remove) != 0 {
		t.Error("transaction state should be reset after second begin")
	}
}

func TestTransactionHelper_Commit_Success(t *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.begin()

	registered := make(map[string]*supervisor.Entry)
	removed := make(map[string]struct{})

	removeFn := func(id string) error {
		removed[id] = struct{}{}
		return nil
	}

	registerFn := func(id string, entry *supervisor.Entry) error {
		registered[id] = entry
		return nil
	}

	assert.NoError(t, th.registerService("service1", &supervisor.Entry{}))
	assert.NoError(t, th.registerService("service2", &supervisor.Entry{}))
	assert.NoError(t, th.removeService("service3"))

	err := th.commit(removeFn, registerFn)
	if err != nil {
		t.Errorf("commit failed: %v", err)
	}

	if len(registered) != 2 {
		t.Errorf("expected 2 services to be registered, got %d", len(registered))
	}
	if len(removed) != 1 {
		t.Errorf("expected 1 service to be removed, got %d", len(removed))
	}

	if _, ok := registered["service1"]; !ok {
		t.Error("service1 should be registered")
	}
	if _, ok := registered["service2"]; !ok {
		t.Error("service2 should be registered")
	}
	if _, ok := removed["service3"]; !ok {
		t.Error("service3 should be removed")
	}

	if th.open {
		t.Error("transaction should be closed after commit")
	}
	if len(th.register) != 0 || len(th.remove) != 0 {
		t.Error("transaction state should be reset after commit")
	}
}

func TestTransactionHelper_Commit_RemoveError(t *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.begin()

	removeFn := func(id string) error {
		if id == "service3" {
			return errors.New("remove error")
		}
		return nil
	}

	registerFn := func(string, *supervisor.Entry) error {
		return nil
	}

	assert.NoError(t, th.removeService("service3"))

	err := th.commit(removeFn, registerFn)
	if err == nil {
		t.Error("commit should have failed")
		return
	}

	expectedError := "failed to remove service service3 during commit: remove error"
	if err.Error() != expectedError {
		t.Errorf("unexpected error message: got '%v', want '%v'", err, expectedError)
	}
}

func TestTransactionHelper_Commit_RegisterError(t *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.begin()

	removeFn := func(_ string) error {
		return nil
	}

	registerFn := func(id string, _ *supervisor.Entry) error {
		if id == "service2" {
			return errors.New("register error")
		}
		return nil
	}

	assert.NoError(t, th.registerService("service1", &supervisor.Entry{}))
	assert.NoError(t, th.registerService("service2", &supervisor.Entry{}))

	err := th.commit(removeFn, registerFn)
	if err == nil {
		t.Error("commit should have failed")
		return
	}

	expectedError := "failed to register service service2 during commit: register error"
	if err.Error() != expectedError {
		t.Errorf("unexpected error message: got '%v', want '%v'", err, expectedError)
	}
}

func TestTransactionHelper_Commit_NoTransaction(t *testing.T) {
	th := newTransactionHelper(noopLogger())

	err := th.commit(nil, nil)
	if err != nil {
		t.Errorf("commit should not return error without active transaction: %v", err)
	}
}

func TestTransactionHelper_Discard(t *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.begin()

	assert.NoError(t, th.registerService("service1", &supervisor.Entry{}))
	assert.NoError(t, th.removeService("service2"))

	th.discard()

	if th.open {
		t.Error("transaction should be closed after discard")
	}
	if len(th.register) != 0 || len(th.remove) != 0 {
		t.Error("transaction state should be reset after discard")
	}
}

func TestTransactionHelper_Discard_NoTransaction(_ *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.discard() // Should not panic or error
}

func TestTransactionHelper_RegisterService(t *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.begin()

	assert.NoError(t, th.registerService("service1", &supervisor.Entry{}))
	if len(th.register) != 1 {
		t.Error("service should be registered")
	}

	// Registering a service that was marked for removal should remove it from th.remove
	assert.NoError(t, th.removeService("service1"))
	assert.NoError(t, th.registerService("service1", &supervisor.Entry{}))
	if len(th.remove) != 0 {
		t.Error("service should have been removed from removal list")
	}
	if len(th.register) != 1 {
		t.Error("service should still be registered")
	}
}

func TestTransactionHelper_RegisterService_NoTransaction(t *testing.T) {
	th := newTransactionHelper(noopLogger())

	err := th.registerService("service1", &supervisor.Entry{})
	if err == nil {
		t.Error("registerService should return error outside of transaction")
	}
}

func TestTransactionHelper_RemoveService(t *testing.T) {
	th := newTransactionHelper(noopLogger())
	th.begin()

	assert.NoError(t, th.removeService("service1"))
	if len(th.remove) != 1 {
		t.Error("service should be marked for removal")
	}

	// Removing a service that was registered should remove it from th.register
	assert.NoError(t, th.registerService("service1", &supervisor.Entry{}))
	assert.NoError(t, th.removeService("service1"))
	if len(th.register) != 0 {
		t.Error("service should have been removed from register list")
	}
	if len(th.remove) != 1 {
		t.Error("service should still be marked for removal")
	}
}

func TestTransactionHelper_RemoveService_NoTransaction(t *testing.T) {
	th := newTransactionHelper(noopLogger())

	err := th.removeService("service1")
	if err == nil {
		t.Error("removeService should return error outside of transaction")
	}
}
