package client

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"

	transcoder "github.com/ponyruntime/pony/system/payload"
	jpayload "github.com/ponyruntime/pony/system/payload/json"
	ypayload "github.com/ponyruntime/pony/system/payload/yaml"
)

func TestFindMatchingLabel(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	ypayload.Register(dtt)

	bridge, err := NewManifestBridge(nil, dtt, nil, 10)
	if err != nil {
		t.Fatalf("NewManifestBridge failed: %v", err)
	}

	t.Run("finds highest matching version", func(t *testing.T) {
		labels := []*modulev1.Label{
			{Name: "1.0.0"},
			{Name: "1.1.0"},
			{Name: "1.2.0"},
			{Name: "2.0.0"},
		}

		constraint, _ := semver.NewConstraint("^1.0.0")
		result, err := bridge.findMatchingLabel(labels, constraint)

		if err != nil {
			t.Fatalf("findMatchingLabel failed: %v", err)
		}

		if result.GetName() != "1.2.0" {
			t.Errorf("expected 1.2.0, got %s", result.GetName())
		}
	})

	t.Run("returns error for no matching version", func(t *testing.T) {
		labels := []*modulev1.Label{
			{Name: "1.0.0"},
			{Name: "1.1.0"},
		}

		constraint, _ := semver.NewConstraint("^2.0.0")
		_, err := bridge.findMatchingLabel(labels, constraint)

		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("skips invalid version labels", func(t *testing.T) {
		labels := []*modulev1.Label{
			{Name: "invalid"},
			{Name: "1.0.0"},
			{Name: "also-invalid"},
		}

		constraint, _ := semver.NewConstraint("^1.0.0")
		result, err := bridge.findMatchingLabel(labels, constraint)

		if err != nil {
			t.Fatalf("findMatchingLabel failed: %v", err)
		}

		if result.GetName() != "1.0.0" {
			t.Errorf("expected 1.0.0, got %s", result.GetName())
		}
	})

	t.Run("selects exact version match", func(t *testing.T) {
		labels := []*modulev1.Label{
			{Name: "1.0.0"},
			{Name: "2.0.0"},
			{Name: "3.0.0"},
		}

		constraint, _ := semver.NewConstraint("2.0.0")
		result, err := bridge.findMatchingLabel(labels, constraint)

		if err != nil {
			t.Fatalf("findMatchingLabel failed: %v", err)
		}

		if result.GetName() != "2.0.0" {
			t.Errorf("expected 2.0.0, got %s", result.GetName())
		}
	})

	t.Run("handles tilde constraint", func(t *testing.T) {
		labels := []*modulev1.Label{
			{Name: "1.2.0"},
			{Name: "1.2.5"},
			{Name: "1.3.0"},
		}

		constraint, _ := semver.NewConstraint("~1.2.0")
		result, err := bridge.findMatchingLabel(labels, constraint)

		if err != nil {
			t.Fatalf("findMatchingLabel failed: %v", err)
		}

		if result.GetName() != "1.2.5" {
			t.Errorf("expected 1.2.5 (highest ~1.2.0), got %s", result.GetName())
		}
	})
}
