// SPDX-License-Identifier: MPL-2.0

package options

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

func TestWorkflowName(t *testing.T) {
	fallback := registry.NewID("app", "Flow")

	t.Run("explicit option", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set(OptionWorkflowName, "ExternalFlow")
		assert.Equal(t, "ExternalFlow", WorkflowName(opts, fallback))
	})

	t.Run("legacy alias", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set("temporal.workflow.name", "LegacyFlow")
		assert.Equal(t, "LegacyFlow", WorkflowName(opts, fallback))
	})

	t.Run("canonical wins over alias", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set(OptionWorkflowName, "Canonical")
		opts.Set("temporal.workflow.name", "Legacy")
		assert.Equal(t, "Canonical", WorkflowName(opts, fallback))
	})

	t.Run("empty option falls back", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set(OptionWorkflowName, "")
		assert.Equal(t, "app:Flow", WorkflowName(opts, fallback))
	})

	t.Run("non-string option falls back", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set(OptionWorkflowName, 123)
		assert.Equal(t, "app:Flow", WorkflowName(opts, fallback))
	})

	t.Run("absent option falls back", func(t *testing.T) {
		assert.Equal(t, "app:Flow", WorkflowName(attrs.NewBag(), fallback))
	})

	t.Run("nil options falls back", func(t *testing.T) {
		assert.Equal(t, "app:Flow", WorkflowName(nil, fallback))
	})

	t.Run("namespace-less fallback has no leading colon", func(t *testing.T) {
		assert.Equal(t, "Flow", WorkflowName(attrs.NewBag(), registry.ParseID("Flow")))
	})
}

func TestWorkflowNameFromID(t *testing.T) {
	assert.Equal(t, "Flow", WorkflowNameFromID(registry.ParseID("Flow")))
	assert.Equal(t, "app:Flow", WorkflowNameFromID(registry.NewID("app", "Flow")))
}

func TestActivityName(t *testing.T) {
	fallback := registry.NewID("app", "DoThing")

	t.Run("explicit option", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set(OptionActivityName, "ExternalThing")
		assert.Equal(t, "ExternalThing", ActivityName(opts, fallback))
	})

	t.Run("legacy alias", func(t *testing.T) {
		opts := attrs.NewBag()
		opts.Set("temporal.activity.name", "LegacyThing")
		assert.Equal(t, "LegacyThing", ActivityName(opts, fallback))
	})

	t.Run("absent option falls back", func(t *testing.T) {
		assert.Equal(t, "app:DoThing", ActivityName(attrs.NewBag(), fallback))
	})

	t.Run("namespace-less fallback has no leading colon", func(t *testing.T) {
		assert.Equal(t, "DoThing", ActivityName(nil, registry.ParseID("DoThing")))
	})
}

func TestActivityNameFromID(t *testing.T) {
	assert.Equal(t, "DoThing", ActivityNameFromID(registry.ParseID("DoThing")))
	assert.Equal(t, "app:DoThing", ActivityNameFromID(registry.NewID("app", "DoThing")))
}
