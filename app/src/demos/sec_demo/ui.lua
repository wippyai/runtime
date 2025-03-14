local http = require("http")

local function handler()
    local res = http.response()
    local req = http.request()

    -- Set HTML content type
    res:set_content_type("text/html")

    -- Single-page UI
    local html = [[
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Security Demo</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
        pre {
            white-space: pre-wrap;
            word-break: break-word;
        }
    </style>
</head>
<body class="bg-gray-50">
    <div class="max-w-4xl mx-auto p-6">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-semibold">Security Demo</h1>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
            <div class="bg-white rounded-lg shadow-md p-6">
                <h2 class="text-lg font-medium mb-4">1. Create Actor</h2>
                <div class="space-y-4">
                    <div>
                        <label class="block text-sm font-medium mb-1">Actor ID</label>
                        <input type="text" id="actor-id" class="w-full px-3 py-2 border border-gray-300 rounded-md" value="user123">
                    </div>
                    <div>
                        <label class="block text-sm font-medium mb-1">Metadata (JSON)</label>
                        <textarea id="actor-metadata" rows="3" class="w-full px-3 py-2 border border-gray-300 rounded-md">{"role": "admin", "org": "example"}</textarea>
                    </div>
                    <button id="create-actor-btn" class="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700">Create Actor</button>
                </div>
            </div>

            <div class="bg-white rounded-lg shadow-md p-6">
                <h2 class="text-lg font-medium mb-4">2. Create Token</h2>
                <div class="space-y-4">
                    <div>
                        <label class="block text-sm font-medium mb-1">Actor ID</label>
                        <input type="text" id="token-actor-id" class="w-full px-3 py-2 border border-gray-300 rounded-md" value="user123">
                    </div>
                    <div>
                        <label class="block text-sm font-medium mb-1">Scope</label>
                        <input type="text" id="token-scope" class="w-full px-3 py-2 border border-gray-300 rounded-md" value="global:admin">
                    </div>
                    <div>
                        <label class="block text-sm font-medium mb-1">Expiration</label>
                        <input type="text" id="token-expiration" class="w-full px-3 py-2 border border-gray-300 rounded-md" value="24h">
                    </div>
                    <button id="create-token-btn" class="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700">Create Token</button>
                </div>
            </div>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <h2 class="text-lg font-medium mb-4">3. Token Operations</h2>
            <div class="space-y-4">
                <div>
                    <label class="block text-sm font-medium mb-1">Token</label>
                    <textarea id="token-value" rows="2" class="w-full px-3 py-2 border border-gray-300 rounded-md" placeholder="Enter token here..."></textarea>
                </div>
                <div class="flex flex-wrap gap-2">
                    <button id="validate-token-btn" class="px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700">Validate Token</button>
                    <button id="revoke-token-btn" class="px-4 py-2 bg-red-600 text-white rounded-md hover:bg-red-700">Revoke Token</button>
                    <button id="access-resource-btn" class="px-4 py-2 bg-purple-600 text-white rounded-md hover:bg-purple-700">Access Restricted Resource</button>
                </div>
            </div>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <h2 class="text-lg font-medium mb-4">4. Check Permission</h2>
            <div class="space-y-4">
                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div>
                        <label class="block text-sm font-medium mb-1">Action</label>
                        <input type="text" id="perm-action" class="w-full px-3 py-2 border border-gray-300 rounded-md" value="read">
                    </div>
                    <div>
                        <label class="block text-sm font-medium mb-1">Resource</label>
                        <input type="text" id="perm-resource" class="w-full px-3 py-2 border border-gray-300 rounded-md" value="demo:restricted">
                    </div>
                </div>
                <div>
                    <label class="block text-sm font-medium mb-1">Metadata (JSON)</label>
                    <textarea id="perm-metadata" rows="2" class="w-full px-3 py-2 border border-gray-300 rounded-md" placeholder='{"owner": "user123"}'></textarea>
                </div>
                <button id="check-permission-btn" class="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700">Check Permission</button>
            </div>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6">
            <h2 class="text-lg font-medium mb-4">Results</h2>
            <div id="results" class="bg-gray-100 rounded-md p-4 min-h-[150px]">
                <div class="text-gray-500">Results will appear here</div>
            </div>
        </div>
    </div>

    <script>
        const API_URL = '/api/demo/security';
        const RESTRICTED_URL = '/api/demo/restricted';
        const resultsDiv = document.getElementById('results');

        // Display results
        function displayResults(data) {
            let html = '<pre class="text-sm">';
            html += JSON.stringify(data, null, 2);
            html += '</pre>';

            if (data.token) {
                html += '<div class="mt-4 p-4 bg-green-100 rounded-lg">';
                html += '<h3 class="font-medium text-green-800">Token Created</h3>';
                html += '<div class="mt-2 break-all">';
                html += `<strong>Token:</strong> <span class="font-mono text-sm">${data.token}</span>`;
                html += '</div>';
                html += '</div>';

                // Auto-fill token field
                document.getElementById('token-value').value = data.token;
            }

            if (data.permission) {
                const allowed = data.permission.allowed === true;
                html += `<div class="mt-4 p-4 ${allowed ? 'bg-green-100' : 'bg-red-100'} rounded-lg">`;
                html += `<h3 class="font-medium ${allowed ? 'text-green-800' : 'text-red-800'}">${allowed ? 'Permission Granted' : 'Permission Denied'}</h3>`;
                html += '</div>';
            }

            resultsDiv.innerHTML = html;
        }

        // API call helper
        async function callApi(url, data, method = 'POST', headers = {}) {
            try {
                const defaultHeaders = {'Content-Type': 'application/json'};
                const response = await fetch(url, {
                    method: method,
                    headers: {...defaultHeaders, ...headers},
                    body: method !== 'GET' ? JSON.stringify(data) : undefined
                });

                const result = await response.json();
                displayResults(result);
                return result;
            } catch (error) {
                displayResults({
                    success: false,
                    error: 'Request failed',
                    details: error.message
                });
            }
        }

        // Create Actor
        document.getElementById('create-actor-btn').addEventListener('click', async () => {
            const id = document.getElementById('actor-id').value;
            let metadata = {};

            try {
                const metadataText = document.getElementById('actor-metadata').value;
                if (metadataText) {
                    metadata = JSON.parse(metadataText);
                }
            } catch (e) {
                return displayResults({
                    success: false,
                    error: 'Invalid metadata JSON',
                    details: e.message
                });
            }

            await callApi(API_URL, {
                action: 'create_actor',
                id,
                metadata
            });

            // Update token actor ID field with the created actor ID
            document.getElementById('token-actor-id').value = id;
        });

        // Create Token
        document.getElementById('create-token-btn').addEventListener('click', async () => {
            const actor_id = document.getElementById('token-actor-id').value;
            const scope = document.getElementById('token-scope').value;
            const expiration = document.getElementById('token-expiration').value;

            await callApi(API_URL, {
                action: 'create_token',
                actor_id,
                scope,
                expiration
            });
        });

        // Validate Token
        document.getElementById('validate-token-btn').addEventListener('click', async () => {
            const token = document.getElementById('token-value').value;

            await callApi(API_URL, {
                action: 'validate_token',
                token
            });
        });

        // Revoke Token
        document.getElementById('revoke-token-btn').addEventListener('click', async () => {
            const token = document.getElementById('token-value').value;

            await callApi(API_URL, {
                action: 'revoke_token',
                token
            });
        });

        // Check Permission
        document.getElementById('check-permission-btn').addEventListener('click', async () => {
            const token = document.getElementById('token-value').value;
            const action = document.getElementById('perm-action').value;
            const resource = document.getElementById('perm-resource').value;
            let metadata = {};

            try {
                const metadataText = document.getElementById('perm-metadata').value;
                if (metadataText) {
                    metadata = JSON.parse(metadataText);
                }
            } catch (e) {
                return displayResults({
                    success: false,
                    error: 'Invalid metadata JSON',
                    details: e.message
                });
            }

            await callApi(API_URL, {
                action: 'check_permission',
                token,
                action,
                resource,
                metadata
            });
        });

        // Access Restricted Resource
        document.getElementById('access-resource-btn').addEventListener('click', async () => {
            const token = document.getElementById('token-value').value;
            const headers = token ? {'Authorization': `Bearer ${token}`} : {};

            await callApi(RESTRICTED_URL, null, 'GET', headers);
        });
    </script>
</body>
</html>
    ]]

    res:write(html)
    return
end

return {
    handler = handler
}