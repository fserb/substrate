#!/usr/bin/env -S deno run --allow-net
// Simple HTTP server for testing substrate transport

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: simple_server.js <host> <port>");
  Deno.exit(1);
}

const server = Deno.serve({ 
  hostname: host === "localhost" ? "127.0.0.1" : host, 
  port: parseInt(port) 
}, (req) => {
  return new Response(`Hello from substrate process!
Host: ${host}
Port: ${port}
URL: ${req.url}
Method: ${req.method}
User-Agent: ${req.headers.get("user-agent") ?? "unknown"}
`, {
    headers: { "Content-Type": "text/plain" }
  });
});

console.log(`Server running at http://${host}:${port}/`);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Received SIGTERM, shutting down gracefully");
  server.shutdown();
  Deno.exit(0);
});