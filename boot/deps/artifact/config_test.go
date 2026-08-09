// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
)

func TestConfiguredRoot(t *testing.T) {
	if got := ConfiguredRoot(nil, ".wippy"); got != ".wippy" {
		t.Fatalf("nil config root = %q", got)
	}

	cfg := boot.NewConfig(boot.WithSection(ConfigName, map[string]any{
		ConfigMaterializationRoot: "build/resources",
	}))
	if got := ConfiguredRoot(cfg, ".wippy"); got != "build/resources" {
		t.Fatalf("configured root = %q", got)
	}
}
