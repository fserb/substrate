#!/usr/bin/env -S deno run --allow-net
// Server that starts slowly to test startup timeout handling

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: slow_startup.js <host> <port>");
  Deno.exit(1);
}

console.log("Starting slow startup process...");

// Simulate slow startup (2 seconds)
await new Promise(resolve => setTimeout(resolve, 2000));

const server = Deno.serve({ 
  hostname: host === "localhost" ? "127.0.0.1" : host, 
  port: parseInt(port) 
}, (req) => {
  return new Response(`Slow startup server ready!
Host: ${host}
Port: ${port}
URL: ${req.url}
`, {
    headers: { "Content-Type": "text/plain" }
  });
});

console.log(`Slow server running at http://${host}:${port}/`);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Slow server received SIGTERM, shutting down");
  server.shutdown();
  Deno.exit(0);
});