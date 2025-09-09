#!/usr/bin/env -S deno run --allow-net
// Server that crashes after serving N requests (default 3)

const [host, port, maxRequests = "3"] = Deno.args;

if (!host || !port) {
  console.error("Usage: crash_server.js <host> <port> [maxRequests]");
  Deno.exit(1);
}

let requestCount = 0;
const maxReq = parseInt(maxRequests);

const server = Deno.serve({ 
  hostname: host === "localhost" ? "127.0.0.1" : host, 
  port: parseInt(port) 
}, (req) => {
  requestCount++;
  
  if (requestCount >= maxReq) {
    console.log(`Crash server handled ${requestCount} requests, crashing now`);
    // Simulate crash
    Deno.exit(1);
  }
  
  return new Response(`Crash server response ${requestCount}/${maxReq}
Host: ${host}
Port: ${port}
URL: ${req.url}
Requests remaining: ${maxReq - requestCount}
`, {
    headers: { "Content-Type": "text/plain" }
  });
});

console.log(`Crash server running at http://${host}:${port}/ (will crash after ${maxReq} requests)`);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Crash server received SIGTERM");
  server.shutdown();
  Deno.exit(0);
});