// Model registry for available models and providers
const modelRegistry = {
    models: [
        {
            id: "gpt-4o",
            name: "GPT-4o",
            providerModel: "gpt-4o-2024-11-20",
            provider: "openai",
            description: "Fast, intelligent, flexible GPT model"
        },
        {
            id: "gpt-4o-mini",
            name: "GPT-4o Mini",
            providerModel: "gpt-4o-mini-2024-07-18",
            provider: "openai",
            description: "Fast, affordable small model"
        },
        {
            id: "o3-mini",
            name: "O3 Mini",
            providerModel: "o3-mini-2025-01-31",
            provider: "openai",
            description: "Fast, flexible intelligent reasoning"
        },
        {
            id: "claude-3-7-sonnet",
            name: "Claude 3.7 Sonnet",
            providerModel: "claude-3-7-sonnet-20250219",
            provider: "anthropic",
            description: "Anthropic's most intelligent model"
        },
        {
            id: "claude-3-5-sonnet",
            name: "Claude 3.5 Sonnet",
            providerModel: "claude-3-5-sonnet-20241022",
            provider: "anthropic",
            description: "High-performance Claude model"
        },
        {
            id: "claude-3-5-haiku",
            name: "Claude 3.5 Haiku",
            providerModel: "claude-3-5-haiku-20241022",
            provider: "anthropic",
            description: "Fastest Claude model"
        }
    ],

    // Get model by ID
    getModel(id) {
        return this.models.find(model => model.id === id);
    },

    // Get provider for a model
    getProvider(id) {
        const model = this.getModel(id);
        return model ? model.provider : null;
    },

    // Get provider model for a model ID
    getProviderModel(id) {
        const model = this.getModel(id);
        return model ? model.providerModel : id; // Fall back to ID if not found
    },

    // Filter models by provider
    getModelsByProvider(provider) {
        return this.models.filter(model => model.provider === provider);
    },

    // Get all models
    getAllModels() {
        return this.models;
    }
};

// Application state
const state = {
    connected: false,
    socket: null,
    reconnectTimer: null,
    reconnectAttempts: 0,
    maxReconnectAttempts: 5,
    messageCount: 0,
    clientCount: 0,
    currentModel: '',
    currentProvider: '',
    systemPrompt: '',
    isStreaming: false,
    currentStreamingMessage: null,
    accumulatedContent: '',
    processedMessageIds: new Set(),
    messageTimestamps: new Map(),
    tokens: {
        prompt: 0,
        completion: 0,
        thinking: 0,
        total: 0
    }
};

// DOM Elements
let statusBadge, tokenInput, connectBtn, disconnectBtn, messageInput, sendBtn,
    chatMessages, debugLog, modelInfo, clientCount, currentModel, currentProvider,
    currentSystemPrompt, connectedClients, messagesHandled, uptime,
    promptTokens, completionTokens, thinkingTokens, totalTokens, tabs, tabContents,
    modelSelector;

// Initialize DOM elements when document is loaded
document.addEventListener('DOMContentLoaded', () => {
    // Get all DOM elements
    statusBadge = document.getElementById('statusBadge');
    tokenInput = document.getElementById('tokenInput');
    connectBtn = document.getElementById('connectBtn');
    disconnectBtn = document.getElementById('disconnectBtn');
    messageInput = document.getElementById('messageInput');
    sendBtn = document.getElementById('sendBtn');
    chatMessages = document.getElementById('chatMessages');
    debugLog = document.getElementById('debugLog');
    modelInfo = document.getElementById('modelInfo');
    clientCount = document.getElementById('clientCount');
    currentModel = document.getElementById('currentModel');
    currentProvider = document.getElementById('currentProvider');
    currentSystemPrompt = document.getElementById('currentSystemPrompt');
    connectedClients = document.getElementById('connectedClients');
    messagesHandled = document.getElementById('messagesHandled');
    uptime = document.getElementById('uptime');
    promptTokens = document.getElementById('promptTokens');
    completionTokens = document.getElementById('completionTokens');
    thinkingTokens = document.getElementById('thinkingTokens');
    totalTokens = document.getElementById('totalTokens');
    tabs = document.querySelectorAll('.tab');
    tabContents = document.querySelectorAll('.tab-content');
    modelSelector = document.getElementById('modelSelector');

    // Set up tab navigation
    setupTabs();

    // Set up model selector
    setupModelSelector();

    // Set up event listeners
    setupEventListeners();

    // Check for token in URL
    checkUrlToken();
});

// Set up tab navigation
function setupTabs() {
    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            const tabName = tab.getAttribute('data-tab');

            // Remove active class from all tabs and contents
            tabs.forEach(t => t.classList.remove('active'));
            tabContents.forEach(c => c.classList.remove('active'));

            // Add active class to clicked tab and corresponding content
            tab.classList.add('active');
            document.getElementById(`${tabName}Tab`).classList.add('active');
        });
    });
}

// Set up model selector dropdown
function setupModelSelector() {
    if (!modelSelector) return;

    // Clear existing options
    modelSelector.innerHTML = '';

    // Add model options grouped by provider
    const providers = {
        'openai': 'OpenAI',
        'anthropic': 'Anthropic'
    };

    // Create option groups for each provider
    for (const [providerId, providerName] of Object.entries(providers)) {
        const group = document.createElement('optgroup');
        group.label = providerName;

        // Add models for this provider
        const models = modelRegistry.getModelsByProvider(providerId);
        models.forEach(model => {
            const option = document.createElement('option');
            option.value = model.id;
            option.textContent = `${model.name} - ${model.description}`;
            option.dataset.provider = model.provider;
            option.dataset.providerModel = model.providerModel;
            group.appendChild(option);
        });

        modelSelector.appendChild(group);
    }

    // Set up event listener for model selection
    modelSelector.addEventListener('change', () => {
        if (!state.connected) return;

        const selectedOption = modelSelector.options[modelSelector.selectedIndex];
        const modelId = selectedOption.value;
        const model = modelRegistry.getModel(modelId);

        if (model) {
            // Send command to change model
            const command = `/model ${model.providerModel}`;
            sendCommand(command);

            // Also update provider if needed
            if (state.currentProvider !== model.provider) {
                setTimeout(() => {
                    const providerCommand = `/provider ${model.provider}`;
                    sendCommand(providerCommand);
                }, 500);
            }
        }
    });
}

// Set up event listeners
function setupEventListeners() {
    connectBtn.addEventListener('click', connect);
    disconnectBtn.addEventListener('click', disconnect);
    sendBtn.addEventListener('click', sendMessage);

    // Allow Enter key to submit
    messageInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            sendMessage();
        }
    });
}

// Check for token in URL
function checkUrlToken() {
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get('token');

    if (token) {
        tokenInput.value = token;
        // Auto-connect if token is provided
        connect();
    }
}

// Helper function to update connection status
function updateStatus(status) {
    statusBadge.className = `status-badge status-${status}`;
    statusBadge.textContent = status.charAt(0).toUpperCase() + status.slice(1);
}

// Helper function to add log entry
function log(type, content) {
    const entry = document.createElement('div');
    entry.className = 'log-entry';

    const time = document.createElement('span');
    time.className = 'log-time';
    time.textContent = `[${new Date().toLocaleTimeString()}] ${type}: `;

    const text = document.createTextNode(
        typeof content === 'object' ? JSON.stringify(content, null, 2) : content
    );

    entry.appendChild(time);
    entry.appendChild(text);
    debugLog.appendChild(entry);

    // Auto-scroll to bottom
    debugLog.scrollTop = debugLog.scrollHeight;
}

// Generate a unique message ID for deduplication
function getMessageId(message) {
    const data = message.data || message;
    const timestamp = data.time || Date.now().toString();

    // For content messages, use content sample + timestamp
    if (data.type === 'content' && data.content) {
        const contentSample = data.content.substring(0, 20).replace(/\s+/g, '');
        return `content_${contentSample}_${timestamp}`;
    }
    // For done messages, use model + timestamp
    else if (data.type === 'done') {
        return `done_${data.model || 'unknown'}_${timestamp}`;
    }
    // For user messages
    else if (data.type === 'user_message' && data.content) {
        const contentSample = data.content.substring(0, 20);
        return `user_${contentSample}_${timestamp}`;
    }

    // Default fallback
    return `${data.type || 'unknown'}_${JSON.stringify(data).substring(0, 30)}_${timestamp}`;
}

// Add a message to the chat
function addMessage(type, content, metadata = {}) {
    const messageEl = document.createElement('div');
    messageEl.className = `message message-${type}`;

    const contentEl = document.createElement('div');
    contentEl.className = 'message-content';

    // Handle content based on type
    if (type === 'assistant' && window.markdownit) {
        try {
            contentEl.innerHTML = window.markdownit().render(content || '');
        } catch (error) {
            contentEl.textContent = content || '';
        }
    } else {
        contentEl.textContent = content || '';
    }

    messageEl.appendChild(contentEl);

    // Add error highlight if needed
    if (type === 'error') {
        messageEl.classList.add('error-highlight');
    }

    // Add metadata if provided
    if (Object.keys(metadata).length > 0) {
        const metaEl = document.createElement('div');
        metaEl.className = 'message-meta';

        if (metadata.model) {
            metaEl.textContent = `${metadata.model} (${metadata.provider || 'unknown'})`;
        } else {
            metaEl.textContent = new Date().toLocaleTimeString();
        }

        messageEl.appendChild(metaEl);
    }

    chatMessages.appendChild(messageEl);
    chatMessages.scrollTop = chatMessages.scrollHeight;

    return messageEl;
}

// Simplified: Create or update a streaming message
function updateStreamingMessage(content, isThinking = false) {
    // Skip if no content to add
    if (!content && !isThinking) return;

    // Create a new message if needed
    if (!state.currentStreamingMessage) {
        state.isStreaming = true;
        state.currentStreamingMessage = addMessage('assistant', '');
        state.accumulatedContent = '';
    }

    // Get the content element
    const contentEl = state.currentStreamingMessage.querySelector('.message-content');

    if (isThinking) {
        // Handle thinking updates
        let thinkingEl = state.currentStreamingMessage.querySelector('.message-thinking');
        if (!thinkingEl) {
            thinkingEl = document.createElement('div');
            thinkingEl.className = 'message-thinking';
            state.currentStreamingMessage.appendChild(thinkingEl);
        }
        thinkingEl.textContent = `Thinking: ${content}`;
    } else {
        // Simply accumulate content
        state.accumulatedContent += content;

        // Update display with full content
        if (window.markdownit) {
            try {
                contentEl.innerHTML = window.markdownit().render(state.accumulatedContent);
            } catch (error) {
                contentEl.textContent = state.accumulatedContent;
            }
        } else {
            contentEl.textContent = state.accumulatedContent;
        }
    }

    // Scroll to bottom
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

// Simplified: Finalize a streaming message
function finalizeStreamingMessage(metadata = {}) {
    if (!state.currentStreamingMessage) return;

    // Remove thinking section if present
    const thinkingEl = state.currentStreamingMessage.querySelector('.message-thinking');
    if (thinkingEl) thinkingEl.remove();

    // Add metadata if provided
    if (Object.keys(metadata).length > 0) {
        let metaEl = state.currentStreamingMessage.querySelector('.message-meta');
        if (!metaEl) {
            metaEl = document.createElement('div');
            metaEl.className = 'message-meta';
            state.currentStreamingMessage.appendChild(metaEl);
        }
        metaEl.textContent = `${metadata.model} (${metadata.provider || 'unknown'})`;
    }

    // Estimate tokens for Anthropic if needed
    if (metadata.provider === 'anthropic' && state.accumulatedContent) {
        const estimatedTokens = Math.ceil(state.accumulatedContent.length / 4);
        state.tokens.completion += estimatedTokens;
        state.tokens.total = state.tokens.prompt + state.tokens.completion + state.tokens.thinking;
        updateTokenStats();
    }

    // Reset state
    state.currentStreamingMessage = null;
    state.isStreaming = false;
    state.accumulatedContent = '';
}

// Update token statistics display
function updateTokenStats() {
    promptTokens.textContent = state.tokens.prompt.toLocaleString();
    completionTokens.textContent = state.tokens.completion.toLocaleString();
    thinkingTokens.textContent = state.tokens.thinking.toLocaleString();
    totalTokens.textContent = state.tokens.total.toLocaleString();
}

// Connect to WebSocket
function connect() {
    const token = tokenInput.value.trim();
    if (!token) {
        alert('Please enter an authentication token');
        return;
    }

    updateStatus('connecting');
    log('System', 'Connecting to WebSocket server...');

    // Get protocol and create WebSocket URL
    const protocol = window.location.protocol.startsWith('https') ? 'wss' : 'ws';
    const wsUrl = `${protocol}://${window.location.host}/api/v1/ws/join?x-auth-token=${encodeURIComponent(token)}`;

    try {
        state.socket = new WebSocket(wsUrl);

        // Connection opened
        state.socket.addEventListener('open', () => {
            state.connected = true;
            updateStatus('connected');
            log('System', 'Connected to WebSocket server');
            addMessage('system', 'Connected to chat server');

            // Update UI
            connectBtn.disabled = true;
            disconnectBtn.disabled = false;
            messageInput.disabled = false;
            sendBtn.disabled = false;
            if (modelSelector) modelSelector.disabled = false;
            state.reconnectAttempts = 0;

            // Clear processed message IDs
            state.processedMessageIds.clear();
            state.messageTimestamps.clear();
        });

        // Message received
        state.socket.addEventListener('message', (event) => {
            let message;
            try {
                message = JSON.parse(event.data);
                log('Received', message);
                handleMessage(message);
            } catch (error) {
                log('Error', `Failed to parse WebSocket message: ${error.message}`);
                log('Raw message', event.data);
            }
        });

        // Connection closed
        state.socket.addEventListener('close', (event) => {
            state.connected = false;
            state.isStreaming = false;
            state.currentStreamingMessage = null;
            state.accumulatedContent = '';

            // Update UI
            updateStatus('disconnected');
            connectBtn.disabled = false;
            disconnectBtn.disabled = true;
            messageInput.disabled = true;
            sendBtn.disabled = true;
            if (modelSelector) modelSelector.disabled = true;

            log('System', `Connection closed. Code: ${event.code}, Reason: ${event.reason || 'No reason provided'}`);
            addMessage('system', `Disconnected from chat server. Code: ${event.code}`);

            // Try to reconnect if not a normal closure
            if (event.code !== 1000 && state.reconnectAttempts < state.maxReconnectAttempts) {
                state.reconnectAttempts++;
                const delay = Math.min(1000 * Math.pow(2, state.reconnectAttempts - 1), 10000);

                log('System', `Reconnecting in ${delay / 1000} seconds... (Attempt ${state.reconnectAttempts}/${state.maxReconnectAttempts})`);
                addMessage('system', `Reconnecting in ${delay / 1000} seconds... (Attempt ${state.reconnectAttempts}/${state.maxReconnectAttempts})`);

                state.reconnectTimer = setTimeout(() => {
                    if (!state.connected) {
                        connect();
                    }
                }, delay);
            }
        });

        // Connection error
        state.socket.addEventListener('error', (event) => {
            log('Error', 'WebSocket connection error');
            addMessage('system', 'Connection error occurred');
        });
    } catch (error) {
        updateStatus('disconnected');
        log('Error', `Failed to create WebSocket: ${error.message}`);
        addMessage('system', `Failed to connect: ${error.message}`);
    }
}

// Disconnect from WebSocket
function disconnect() {
    if (state.reconnectTimer) {
        clearTimeout(state.reconnectTimer);
        state.reconnectTimer = null;
    }

    if (state.socket) {
        state.socket.close(1000, "User disconnected");
        state.socket = null;
    }
}

// Handle incoming messages
function handleMessage(message) {
    // Extract topic and create message ID
    const topic = message.topic || '';
    const messageId = getMessageId(message);

    // Add timestamp-based deduplication
    const now = Date.now();
    const lastProcessedTime = state.messageTimestamps.get(messageId);

    if (lastProcessedTime && (now - lastProcessedTime < 500)) {
        log('Skipped', 'Duplicate message received within 500ms: ' + messageId);
        return;
    }

    // Store timestamp of this message
    state.messageTimestamps.set(messageId, now);

    if (topic === 'welcome') {
        // Handle welcome message
        log('Welcome', message);

        // Extract data from the welcome message
        const data = message.data || message;
        addMessage('system', `Welcome! User ID: ${data.user_id || 'Unknown'}`);

        // Update client info
        if (data.client_count) {
            state.clientCount = data.client_count;
            clientCount.textContent = `Clients: ${state.clientCount}`;
            connectedClients.textContent = state.clientCount;
        }

        // Update model info
        if (data.model) {
            state.currentModel = data.model;
            state.currentProvider = data.provider || 'unknown';
            modelInfo.textContent = `Model: ${state.currentModel} (${state.currentProvider})`;
            currentModel.textContent = state.currentModel;
            currentProvider.textContent = state.currentProvider;

            // Update model selector if it exists
            if (modelSelector) {
                // Find the matching model in the registry
                const registryModel = modelRegistry.models.find(m =>
                    m.providerModel === data.model || m.id === data.model);

                if (registryModel) {
                    modelSelector.value = registryModel.id;
                }
            }
        }

        // Update system prompt
        if (data.system_prompt) {
            state.systemPrompt = data.system_prompt;
            currentSystemPrompt.textContent = state.systemPrompt;
        }

        // Update token info
        if (data.tokens) {
            state.tokens = data.tokens;
            updateTokenStats();
        }
    } else if (topic === 'update') {
        // Handle update messages - extract the data field
        const data = message.data || message;

        // Handle different update types
        const type = data.type || '';

        switch (type) {
            case 'start':
                // LLM session start
                state.isStreaming = true;
                if (data.model) state.currentModel = data.model;
                if (data.provider) state.currentProvider = data.provider;
                modelInfo.textContent = `Model: ${state.currentModel} (${state.currentProvider})`;
                break;

            case 'content':
                // Content update from LLM
                if (data.content && data.content.length > 0) {
                    updateStreamingMessage(data.content);
                }
                break;

            case 'thinking':
                // Thinking output
                updateStreamingMessage(data.content || '', true);
                break;

            case 'done':
                // LLM response complete
                finalizeStreamingMessage({
                    model: data.model || state.currentModel,
                    provider: data.provider || state.currentProvider
                });
                break;

            case 'error':
                // Error notification
                if (state.isStreaming) {
                    finalizeStreamingMessage();
                }

                let errorMessage = data.message || data.error || "Unknown error occurred";
                if (data.error && typeof data.error === 'object') {
                    errorMessage = data.error.message || JSON.stringify(data.error);
                }

                addMessage('error', `Error: ${errorMessage}`);
                break;

            case 'system':
                // System message
                addMessage('system', data.message);
                break;

            case 'user_message':
                // Echo of user message
                addMessage('user', data.content);
                break;

            case 'tokens':
                // Token statistics update
                if (data.total) {
                    state.tokens = {
                        prompt: data.total.prompt || state.tokens.prompt || 0,
                        completion: data.total.completion || state.tokens.completion || 0,
                        thinking: data.total.thinking || state.tokens.thinking || 0,
                        total: data.total.total || state.tokens.total || 0
                    };
                }

                // Handle token updates for Anthropic vs OpenAI
                if (data.session) {
                    if (data.session.total === 0 && state.currentProvider === 'anthropic') {
                        // For Anthropic, tokens will be estimated in finalizeStreamingMessage
                    } else {
                        // For OpenAI (or if actual values provided)
                        state.tokens.prompt += (data.session.prompt || 0);
                        state.tokens.completion += (data.session.completion || 0);
                        state.tokens.thinking += (data.session.thinking || 0);
                        state.tokens.total = state.tokens.prompt + state.tokens.completion + state.tokens.thinking;
                    }
                }

                updateTokenStats();
                break;

            case 'stats':
                // Statistics update
                if (data.clients) {
                    state.clientCount = data.clients;
                    clientCount.textContent = `Clients: ${state.clientCount}`;
                    connectedClients.textContent = state.clientCount;
                }

                if (data.messages) {
                    messagesHandled.textContent = data.messages;
                }

                if (data.uptime) {
                    uptime.textContent = data.uptime;
                }

                if (data.tokens) {
                    state.tokens = data.tokens;
                    updateTokenStats();
                }
                break;
        }
    } else if (topic === 'llm_response') {
        // Handle direct llm_response messages
        const data = message.data || message;

        if (data.type === 'content' && data.content) {
            updateStreamingMessage(data.content);
        } else if (data.type === 'thinking' && data.thinking) {
            updateStreamingMessage(data.thinking, true);
        } else if (data.type === 'done') {
            const meta = data.meta || {};
            finalizeStreamingMessage({
                model: meta.model || state.currentModel,
                provider: meta.provider || state.currentProvider
            });

            // Update token statistics if available
            if (meta && meta.usage) {
                const usage = meta.usage;
                state.tokens.prompt += usage.prompt_tokens || 0;
                state.tokens.completion += usage.completion_tokens || 0;

                if (usage.completion_tokens_details && usage.completion_tokens_details.reasoning_tokens) {
                    state.tokens.thinking += usage.completion_tokens_details.reasoning_tokens;
                }

                state.tokens.total = state.tokens.prompt + state.tokens.completion + state.tokens.thinking;
                updateTokenStats();
            }
        }
    } else if (topic === 'ws.cancel') {
        // Handle cancellation
        const reason = message.data && message.data.reason ? message.data.reason : 'No reason provided';
        addMessage('system', `Chat server is shutting down: ${reason}`);
        disconnect();
    } else {
        // Other message types are just logged
        log('Other', message);
    }
}

// Send a command to the server
function sendCommand(command) {
    if (!state.connected || !state.socket) {
        addMessage('system', 'Not connected to the server');
        return;
    }

    try {
        const payload = {
            text: command
        };

        state.socket.send(JSON.stringify(payload));
        log('Sent Command', payload);
    } catch (error) {
        log('Error', `Failed to send command: ${error.message}`);
        addMessage('system', `Error sending command: ${error.message}`);
    }
}

// Send a message
function sendMessage() {
    if (!state.connected || !state.socket) {
        alert('Not connected to the server');
        return;
    }

    const text = messageInput.value.trim();
    if (!text) {
        return;
    }

    try {
        const payload = {
            text: text
        };

        state.socket.send(JSON.stringify(payload));
        log('Sent', payload);

        // Clear input
        messageInput.value = '';
    } catch (error) {
        log('Error', `Failed to send message: ${error.message}`);
        addMessage('system', `Error sending message: ${error.message}`);
    }
}