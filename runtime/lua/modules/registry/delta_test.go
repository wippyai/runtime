package registry

import (
	"testing"

	"go.uber.org/zap"
)

func TestMakeBuildDelta(t *testing.T) {
	log := zap.NewNop()
	fn := makeBuildDelta(log)

	if fn == nil {
		t.Error("expected non-nil function")
	}
}

func TestMakeBuildDeltaNilLogger(t *testing.T) {
	fn := makeBuildDelta(nil)

	if fn == nil {
		t.Error("expected non-nil function even with nil logger")
	}
}
