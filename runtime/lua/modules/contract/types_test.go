// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestTypedContractClient(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.Check.Strict = true
	checker := code.NewTypeChecker(config, nil)
	imports := map[string]*io.Manifest{"contract": ModuleTypes()}

	const source = `
local contract = require("contract")
type Request = { id: string }
type Result = { value: integer }
type Client = { read: (self: Client, input: Request) -> (Result?, error?) }

local function read(id: string): (Result?, error?)
    local definition, err = contract.get("test:capability", Client)
    if not definition then return nil, err end
    local client, open_err = definition:with_context({ board = id }):open()
    if not client then return nil, open_err end
    return client:read({ id = id })
end

return { read = read }
`
	_, diagnostics, err := checker.Check(source, "typed_contract", imports)
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "diagnostics: %v", diagnostics)

	for name, expression := range map[string]string{
		"wrong request": "return client:read({ id = 1 })",
		"wrong result":  "local result = client:read({ id = id }); return result and result.value, nil",
		"wrong error":   "local _, err = client:read({ id = id }); local message: string? = err; return nil, nil",
	} {
		t.Run(name, func(t *testing.T) {
			invalid := strings.Replace(source, "return client:read({ id = id })", expression, 1)
			_, diagnostics, err := checker.Check(invalid, "typed_contract_invalid", imports)
			require.NoError(t, err)
			require.True(t, code.HasErrors(diagnostics), "diagnostics: %v", diagnostics)
		})
	}
}
