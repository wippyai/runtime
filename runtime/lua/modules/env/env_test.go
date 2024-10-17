package env_test

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/modules/env"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestKeyRetrival(t *testing.T) {
	luacode := `
local env = require("env")

local api_token = env.get("jira_access_token")
if not api_token then
    return {
        success = false,
        message = "Failed to retrieve Jira API token from environment key"
    }
end

-- Ensure the Jira URL doesn't end with a slash
if jira_url:sub(-1) == "/" then
    jira_url = jira_url:sub(1, -2)
end

return "foo"
`

	lg, _ := zap.NewDevelopment()
	le := engine.NewLuaEngine(context.Background(), lg)
	m := make(map[string]string)
	m["jira_access_token"] = "https://example.atlassian.net"

	module := env.NewEnvKeysModule(nil, "authToken", m, lg)
	le.L.PreloadModule("env", module.Loader)

	err := le.DoString(luacode, "<string>")
	require.NoError(t, err)
}
