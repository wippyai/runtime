// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"testing"

	apierror "github.com/wippyai/runtime/api/error"
)

func TestL10LuaErrorMetadataProjectionAndDetach(t *testing.T) {
	details := map[string]any{"attempt": 7, "region": "test-zone"}
	err := apierror.NewRich(apierror.Kind("TransientLiteral"), "temporary failure").
		WithRetryable(apierror.True).
		WithDetails(details)

	metadata := ErrorMetadata(err)
	if metadata == nil {
		t.Fatal("ErrorMetadata() = nil")
	}
	if string(metadata.Kind) != "TransientLiteral" || metadata.Retryable == nil || !*metadata.Retryable {
		t.Fatalf("metadata fields = %#v", metadata)
	}
	if metadata.Details["attempt"] != 7 || metadata.Details["region"] != "test-zone" {
		t.Fatalf("metadata details = %#v", metadata.Details)
	}
	details["attempt"] = 8
	details["region"] = "changed"
	if metadata.Details["attempt"] != 7 || metadata.Details["region"] != "test-zone" {
		t.Fatalf("metadata details changed with source = %#v", metadata.Details)
	}
}

func TestL11LuaErrorMetadataNilCases(t *testing.T) {
	inputs := []error{nil, apierror.New(apierror.Kind(""), "metadata free")}
	for index, input := range inputs {
		if got := ErrorMetadata(input); got != nil {
			t.Fatalf("fixture %d metadata = %#v, want nil", index, got)
		}
	}
}
