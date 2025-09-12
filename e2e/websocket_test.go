package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestBasicWebSocketEcho(t *testing.T) {
	serverBlock := `@ws_files {
		path *.ws.js
		file {path}
	}

	reverse_proxy @ws_files {
		transport substrate
		to localhost
	}`

	wsServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	if (req.headers.get("upgrade") === "websocket") {
		const { socket, response } = Deno.upgradeWebSocket(req);

		socket.onmessage = (event) => {
			socket.send("Echo: " + event.data);
		};

		return response;
	} else {
		return new Response("WebSocket endpoint", { status: 200 });
	}
});`

	files := []TestFile{
		{Path: "echo.ws.js", Content: wsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	time.Sleep(100 * time.Millisecond)

	wsURL := fmt.Sprintf("ws://localhost:%d/echo.ws.js", ctx.HTTPPort)
	origin := ctx.BaseURL

	conn, err := websocket.Dial(wsURL, "", origin)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	testMessage := "Hello WebSocket!"
	if err := websocket.Message.Send(conn, testMessage); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	var response string
	if err := websocket.Message.Receive(conn, &response); err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	expectedResponse := "Echo: " + testMessage
	if response != expectedResponse {
		t.Errorf("Expected %q, got %q", expectedResponse, response)
	}
}

func TestWebSocketBidirectionalCommunication(t *testing.T) {
	serverBlock := `@ws_files {
		path *.ws.js
		file {path}
	}

	reverse_proxy @ws_files {
		transport substrate
		to localhost
	}`

	wsServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	if (req.headers.get("upgrade") === "websocket") {
		const { socket, response } = Deno.upgradeWebSocket(req);
		let messageCount = 0;
		let interval;

		socket.onopen = () => {
			socket.send("Welcome to WebSocket server!");

			interval = setInterval(() => {
				if (socket.readyState === WebSocket.OPEN) {
					messageCount++;
					socket.send(` + "`" + `Server message ${messageCount}` + "`" + `);
				}
			}, 200);
		};

		socket.onmessage = (event) => {
			socket.send("Server received: " + event.data);
		};

		socket.onclose = () => {
			if (interval) clearInterval(interval);
		};

		socket.onerror = (error) => {
			if (interval) clearInterval(interval);
		};

		return response;
	} else {
		return new Response("WebSocket endpoint", { status: 200 });
	}
});`

	files := []TestFile{
		{Path: "bidirectional.ws.js", Content: wsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	time.Sleep(100 * time.Millisecond)

	wsURL := fmt.Sprintf("ws://localhost:%d/bidirectional.ws.js", ctx.HTTPPort)
	origin := ctx.BaseURL

	conn, err := websocket.Dial(wsURL, "", origin)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	var welcomeMsg string
	if err := websocket.Message.Receive(conn, &welcomeMsg); err != nil {
		t.Fatalf("Failed to receive welcome message: %v", err)
	}

	if welcomeMsg != "Welcome to WebSocket server!" {
		t.Errorf("Expected welcome message, got %q", welcomeMsg)
	}

	clientMessage := "Hello from client!"
	if err := websocket.Message.Send(conn, clientMessage); err != nil {
		t.Fatalf("Failed to send client message: %v", err)
	}

	var echoResponse string
	if err := websocket.Message.Receive(conn, &echoResponse); err != nil {
		t.Fatalf("Failed to receive echo response: %v", err)
	}

	expectedEcho := "Server received: " + clientMessage
	if echoResponse != expectedEcho {
		t.Errorf("Expected echo %q, got %q", expectedEcho, echoResponse)
	}

	serverMessages := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		var serverMsg string
		if err := websocket.Message.Receive(conn, &serverMsg); err != nil {
			t.Fatalf("Failed to receive server message %d: %v", i+1, err)
		}
		serverMessages = append(serverMessages, serverMsg)
	}

	for i, msg := range serverMessages {
		expectedMsg := fmt.Sprintf("Server message %d", i+1)
		if msg != expectedMsg {
			t.Errorf("Expected server message %d to be %q, got %q", i+1, expectedMsg, msg)
		}
	}
}

func TestConcurrentWebSocketConnections(t *testing.T) {
	serverBlock := `@ws_files {
		path *.ws.js
		file {path}
	}

	reverse_proxy @ws_files {
		transport substrate
		to localhost
	}`

	wsServer := `#!/usr/bin/env -S deno run --allow-net
let connectionCount = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	if (req.headers.get("upgrade") === "websocket") {
		const { socket, response } = Deno.upgradeWebSocket(req);

		socket.onopen = () => {
			connectionCount++;
			socket.send(` + "`" + `Connection ID: ${connectionCount}` + "`" + `);
		};

		socket.onmessage = (event) => {
			const message = event.data;
			const start = message.indexOf("Connection ") + "Connection ".length;
			const end = message.indexOf(":");
			const connId = start > 0 && end > start ? message.substring(start, end) : "unknown";
			socket.send(` + "`" + `Response to connection ${connId}: ${message}` + "`" + `);
		};

		return response;
	} else {
		return new Response("WebSocket endpoint", { status: 200 });
	}
});`

	files := []TestFile{
		{Path: "concurrent.ws.js", Content: wsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	time.Sleep(100 * time.Millisecond)

	numConnections := 3
	connections := make([]*websocket.Conn, numConnections)
	connectionIDs := make([]string, numConnections)

	wsURL := fmt.Sprintf("ws://localhost:%d/concurrent.ws.js", ctx.HTTPPort)
	origin := ctx.BaseURL

	for i := 0; i < numConnections; i++ {
		conn, err := websocket.Dial(wsURL, "", origin)
		if err != nil {
			t.Fatalf("Failed to connect to WebSocket %d: %v", i+1, err)
		}
		connections[i] = conn
		defer conn.Close()

		var connIDMsg string
		if err := websocket.Message.Receive(conn, &connIDMsg); err != nil {
			t.Fatalf("Failed to receive connection ID for connection %d: %v", i+1, err)
		}

		parts := strings.Split(connIDMsg, ": ")
		if len(parts) != 2 || parts[0] != "Connection ID" {
			t.Fatalf("Unexpected connection ID format: %q", connIDMsg)
		}
		connectionIDs[i] = parts[1]
	}

	for i, conn := range connections {
		connID := connectionIDs[i]
		message := fmt.Sprintf("Connection %s: Hello from client %d!", connID, i+1)

		if err := websocket.Message.Send(conn, message); err != nil {
			t.Fatalf("Failed to send message from connection %d: %v", i+1, err)
		}

		var response string
		if err := websocket.Message.Receive(conn, &response); err != nil {
			t.Fatalf("Failed to receive response for connection %d: %v", i+1, err)
		}

		expectedResponse := fmt.Sprintf("Response to connection %s: %s", connID, message)
		if response != expectedResponse {
			t.Errorf("Connection %d: expected %q, got %q", i+1, expectedResponse, response)
		}
	}
}

func TestWebSocketConnectionPersistence(t *testing.T) {
	serverBlock := `@ws_files {
		path *.ws.js
		file {path}
	}

	reverse_proxy @ws_files {
		transport substrate
		to localhost
	}`

	wsServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	if (req.headers.get("upgrade") === "websocket") {
		const { socket, response } = Deno.upgradeWebSocket(req);
		let messageCount = 0;

		socket.onmessage = (event) => {
			messageCount++;
			const message = event.data;

			if (message === "ping") {
				socket.send(` + "`" + `pong ${messageCount}` + "`" + `);
			} else if (message === "count") {
				socket.send(` + "`" + `Message count: ${messageCount}` + "`" + `);
			} else {
				socket.send(` + "`" + `Received message ${messageCount}: ${message}` + "`" + `);
			}
		};

		return response;
	} else {
		return new Response("WebSocket endpoint", { status: 200 });
	}
});`

	files := []TestFile{
		{Path: "persistent.ws.js", Content: wsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	time.Sleep(100 * time.Millisecond)

	wsURL := fmt.Sprintf("ws://localhost:%d/persistent.ws.js", ctx.HTTPPort)
	origin := ctx.BaseURL

	conn, err := websocket.Dial(wsURL, "", origin)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	messages := []string{"Hello", "ping", "World", "ping", "Test"}

	for i, msg := range messages {
		if err := websocket.Message.Send(conn, msg); err != nil {
			t.Fatalf("Failed to send message %d (%s): %v", i+1, msg, err)
		}

		var response string
		if err := websocket.Message.Receive(conn, &response); err != nil {
			t.Fatalf("Failed to receive response for message %d: %v", i+1, err)
		}

		if msg == "ping" {
			expectedResponse := fmt.Sprintf("pong %d", i+1)
			if response != expectedResponse {
				t.Errorf("Message %d: expected %q, got %q", i+1, expectedResponse, response)
			}
		} else {
			expectedResponse := fmt.Sprintf("Received message %d: %s", i+1, msg)
			if response != expectedResponse {
				t.Errorf("Message %d: expected %q, got %q", i+1, expectedResponse, response)
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	if err := websocket.Message.Send(conn, "count"); err != nil {
		t.Fatalf("Failed to send count message: %v", err)
	}

	var countResponse string
	if err := websocket.Message.Receive(conn, &countResponse); err != nil {
		t.Fatalf("Failed to receive count response: %v", err)
	}

	expectedCountResponse := "Message count: 6"
	if countResponse != expectedCountResponse {
		t.Errorf("Expected count %q, got %q", expectedCountResponse, countResponse)
	}
}

