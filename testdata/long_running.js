#!/usr/bin/env -S deno run --allow-net
// Long running server for testing idle timeout behavior

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: long_running.js <host> <port>");
  Deno.exit(1);
}

let requestCount = 0;
const startTime = Date.now();

const server = Deno.serve({ 
  hostname: host, 
  port: parseInt(port) 
}, (req) => {
  requestCount++;
  const uptime = Math.floor((Date.now() - startTime) / 1000);
  
  return new Response(`Long running server
Host: ${host}
Port: ${port}
URL: ${req.url}
Request count: ${requestCount}
Uptime: ${uptime} seconds
Started: ${new Date(startTime).toISOString()}
`, {
    headers: { "Content-Type": "text/plain" }
  });
});

console.log(`Long running server started at http://${host}:${port}/`);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  const uptime = Math.floor((Date.now() - startTime) / 1000);
  console.log(`Long running server received SIGTERM after ${uptime}s uptime, ${requestCount} requests`);
  server.shutdown();
  Deno.exit(0);
});