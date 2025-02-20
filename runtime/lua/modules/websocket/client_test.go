package websocket

import (
	"context"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// startWSServer spins up a test server using the provided HTTP handler.
// The returned URL will be a ws:// URL pointing to the given path.
func startWSServer(t *testing.T, path string, handler http.HandlerFunc) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// Convert the server’s URL (which is http://) to a WebSocket URL.
	return srv
}

func TestWebSocketClient(t *testing.T) {
	logger := zap.NewNop()
	wsModule := NewWebSocketModule(logger)

	// --- Basic connection and message exchange ---
	t.Run("basic connection and message exchange", func(t *testing.T) {
		// The server upgrades the connection and waits for a client message.
		// When it receives a text message, it replies with "Server Response".
		srv := startWSServer(t, "/ws", func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				t.Errorf("Accept error: %v", err)
				return
			}
			defer func() { assert.NoError(t, conn.Close(coderws.StatusNormalClosure, "server closing")) }()

			// Read one message from the client.
			msgType, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			// If the client sent a text message, reply.
			if msgType == coderws.MessageText {
				_ = conn.Write(r.Context(), coderws.MessageText, []byte("Server Response"))
			}
			// Continue reading (so that the client’s close is observed).
			for {
				_, _, err = conn.Read(r.Context())
				if err != nil {
					break
				}
			}
		})
		// Construct the ws URL (replace "http" with "ws" and add the path)
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("websocket", wsModule.Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            function test()
                local ws = websocket.connect("` + wsURL + `", {
                    headers = { ["User-Agent"] = "Lua WebSocket Client" },
                    dial_timeout = "5s",
                    read_timeout = "30s",
                    write_timeout = "10s"
                })
                
                local ok, err = ws:send("Hello WebSocket!")
                assert(ok, "Failed to send message")
                
                local ch = ws:receive()
                local msg = ch:receive()
                assert(msg.type == websocket.TYPE_TEXT, "Expected type text")
                assert(msg.data == "Server Response", "Unexpected response")
                
                ok, err = ws:close(websocket.CLOSE_CODES.NORMAL, "Goodbye")
                assert(ok, "Failed to close connection")
                
                return "success"
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelLayer()
		asyncRunner := async.NewAsyncLayer(channels, 4096)
		runner := engine.NewRunner(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)
		ctx := engine.WithTaskGroup(context.Background(), runner.GetTaskGroup())
		ctx = asyncRunner.WithContext(ctx)
		result, err := runner.Execute(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})

	// --- Connection timeout ---
	t.Run("connection timeout", func(t *testing.T) {
		// This handler deliberately delays the upgrade so that the client’s dial_timeout expires.
		srv := startWSServer(t, "/ws", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond) // longer than the dial timeout below
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				return
			}
			_ = conn.Close(coderws.StatusNormalClosure, "late")
		})
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("websocket", wsModule.Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            function test()
                local ws, err = websocket.connect("` + wsURL + `", {
                    dial_timeout = "100ms"  -- Very short timeout
                })
                
                assert(ws == nil, "Expected ws to be nil due to timeout")
                assert(err ~= nil, "Expected error due to timeout")
                assert(string.find(err, "deadline exceeded") ~= nil, "Error should mention timeout")
                return "success"
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelLayer()
		asyncRunner := async.NewAsyncLayer(channels, 4096)
		runner := engine.NewRunner(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)
		ctx := engine.WithTaskGroup(context.Background(), runner.GetTaskGroup())
		ctx = asyncRunner.WithContext(ctx)
		result, err := runner.Execute(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})

	// --- Message types ---
	t.Run("message types", func(t *testing.T) {
		// This server sends a series of frames: text, binary, ping, pong, then closes.
		srv := startWSServer(t, "/ws", func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				return
			}
			ctx := r.Context()
			// Send a text message.
			_ = conn.Write(ctx, coderws.MessageText, []byte("Hello"))
			time.Sleep(10 * time.Millisecond)
			// Send a binary message.
			_ = conn.Write(ctx, coderws.MessageBinary, []byte("Binary Data"))
			time.Sleep(10 * time.Millisecond)
			// Close with a normal closure.
			_ = conn.Close(coderws.StatusNormalClosure, "normal closure")
		})
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("websocket", wsModule.Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// The Lua test expects to see five messages in order.
		script := `
            function test()
                local ws = websocket.connect("` + wsURL + `")
                local ch = ws:receive()
                local messages = {
                    { type = websocket.TYPE_TEXT, data = "Hello" },
                    { type = websocket.TYPE_BINARY, data = "Binary Data" },
                    { type = websocket.TYPE_CLOSE, code = websocket.CLOSE_CODES.NORMAL, reason = "normal closure" }
                }
                for _, expected in ipairs(messages) do
                    local msg = ch:receive()
                    assert(msg.type == expected.type, "Expected "..expected.type.." got "..tostring(msg.type))
                    if expected.data then
                        assert(msg.data == expected.data, "Data mismatch")
                    end
                    if expected.code then
                        assert(msg.code == expected.code, "Close code mismatch")
                    end
                end
                return "success"
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelLayer()
		asyncRunner := async.NewAsyncLayer(channels, 4096)
		runner := engine.NewRunner(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)
		ctx := engine.WithTaskGroup(context.Background(), runner.GetTaskGroup())
		ctx = asyncRunner.WithContext(ctx)
		result, err := runner.Execute(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})

	// --- Concurrent operations ---
	t.Run("concurrent operations", func(t *testing.T) {
		// This echo server reads any incoming message and immediately writes it back.
		srv := startWSServer(t, "/ws", func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { assert.NoError(t, conn.Close(coderws.StatusNormalClosure, "closing")) }()
			for {
				msgType, data, err := conn.Read(r.Context())
				if err != nil {
					break
				}
				_ = conn.Write(r.Context(), msgType, data)
			}
		})
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", timemod.NewTimeModule().Loader),
			engine.WithPreloaded("websocket", wsModule.Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            function test()
                local ws = websocket.connect("` + wsURL + `")
                local ch = ws:receive()
                local done = channel.new(0)
                
                coroutine.spawn(function()
                    for i = 1, 3 do
                        ws:send("Message " .. i)
                        time.after("100ms"):receive()
                    end
                    done:send(true)
                end)
                
                coroutine.spawn(function()
                    local messages = {}
                    for i = 1, 3 do
                        local msg = ch:receive()
                        table.insert(messages, msg.data)
                    end
                    assert(#messages == 3, "Expected 3 messages")
                    done:send(true)
                end)
                
                done:receive()
                done:receive()
                return "success"
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelLayer()
		asyncRunner := async.NewAsyncLayer(channels, 4096)
		runner := engine.NewRunner(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)
		ctx := engine.WithTaskGroup(context.Background(), runner.GetTaskGroup())
		ctx = asyncRunner.WithContext(ctx)
		result, err := runner.Execute(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})
}

func TestWebSocketModule(t *testing.T) {
	logger := zap.NewNop()
	wsModule := NewWebSocketModule(logger)

	// --- Module loading ---
	t.Run("module loading", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("websocket", wsModule.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            function test() 
                assert(type(websocket) == "table")
                assert(type(websocket.connect) == "function")
                
                assert(websocket.TYPE_TEXT == "text")
                assert(websocket.TYPE_BINARY == "binary")
                assert(websocket.TYPE_PING == "ping")
                assert(websocket.TYPE_PONG == "pong")
                assert(websocket.TYPE_CLOSE == "close")
                
                assert(websocket.CLOSE_CODES.NORMAL == 1000)
                assert(websocket.CLOSE_CODES.GOING_AWAY == 1001)
                assert(websocket.CLOSE_CODES.PROTOCOL_ERROR == 1002)
                
                return "success"
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelLayer()
		asyncRunner := async.NewAsyncLayer(channels, 4096)
		runner := engine.NewRunner(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)
		ctx := engine.WithTaskGroup(context.Background(), runner.GetTaskGroup())
		ctx = asyncRunner.WithContext(ctx)
		result, err := runner.Execute(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})

	// --- Compression options ---
	t.Run("compression options", func(t *testing.T) {
		// A minimal server that accepts the connection.
		srv := startWSServer(t, "/ws", func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				return
			}
			_ = conn.Close(coderws.StatusNormalClosure, "closing")
		})
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("websocket", wsModule.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            function test() 
                local ws1 = websocket.connect("` + wsURL + `", {
                    compression = "context_takeover",
                    compression_threshold = 1024
                })
                
                local ws2 = websocket.connect("` + wsURL + `", {
                    compression = "no_context_takeover"
                })
                
                local ws3 = websocket.connect("` + wsURL + `", {
                    compression = "disabled"
                })
                
                return "success"
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelLayer()
		asyncRunner := async.NewAsyncLayer(channels, 4096)
		runner := engine.NewRunner(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)
		ctx := engine.WithTaskGroup(context.Background(), runner.GetTaskGroup())
		ctx = asyncRunner.WithContext(ctx)
		result, err := runner.Execute(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})
}
