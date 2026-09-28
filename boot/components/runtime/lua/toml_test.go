// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"testing"

	"go.uber.org/zap"
)

func TestTOMLComponentRegistersModule(t *testing.T) {
	ctx, _ := engineTestContext(t, zap.NewNop())
	loaded, err := Engine().Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = TOML().Load(loaded)
	if err != nil {
		t.Fatal(err)
	}
	manager := GetCodeManager(loaded)
	if manager == nil {
		t.Fatal("TOML component lost the code manager")
	}
	for _, module := range manager.GetModuleDefs() {
		if module.Name == "toml" {
			return
		}
	}
	t.Fatal("TOML component did not register the toml module")
}
