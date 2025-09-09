#!/usr/bin/env -S deno run --allow-net
// Server that demonstrates proper graceful shutdown handling

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: graceful_shutdown.js <host> <port>");
  Deno.exit(1);
}

let shutdownRequested = false;
let activeConnections = 0;

const server = Deno.serve({ 
  hostname: host === "localhost" ? "127.0.0.1" : host, 
  port: parseInt(port) 
}, async (req) => {
  activeConnections++;
  
  // Simulate some processing time
  if (req.url.includes("/slow")) {
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  
  const response = new Response(`Graceful shutdown server
Host: ${host}
Port: ${port}
URL: ${req.url}
Active connections: ${activeConnections}
Shutdown requested: ${shutdownRequested}
`, {
    headers: { "Content-Type": "text/plain" }
  });
  
  activeConnections--;
  return response;
});

console.log(`Graceful shutdown server running at http://${host}:${port}/`);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Graceful shutdown server received SIGTERM");
  shutdownRequested = true;
  
  // Give active connections time to finish
  const shutdownTimeout = setTimeout(() => {
    console.log("Graceful shutdown timeout, forcing exit");
    Deno.exit(1);
  }, 5000);
  
  server.shutdown().then(() => {
    clearTimeout(shutdownTimeout);
    console.log("Graceful shutdown completed successfully");
    Deno.exit(0);
  });
});