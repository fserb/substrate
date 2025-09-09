#!/usr/bin/env -S deno run --allow-net
// Server that echoes request details for testing header/body forwarding

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: echo_server.js <host> <port>");
  Deno.exit(1);
}

const server = Deno.serve({ 
  hostname: host === "localhost" ? "127.0.0.1" : host, 
  port: parseInt(port) 
}, async (req) => {
  const url = new URL(req.url);
  const body = req.method === "POST" || req.method === "PUT" 
    ? await req.text() 
    : "";
  
  const headers = {};
  for (const [key, value] of req.headers.entries()) {
    headers[key] = value;
  }
  
  const response = {
    method: req.method,
    url: req.url,
    pathname: url.pathname,
    search: url.search,
    headers: headers,
    body: body,
    serverInfo: {
      host: host,
      port: port,
      timestamp: new Date().toISOString()
    }
  };
  
  return new Response(JSON.stringify(response, null, 2), {
    headers: { 
      "Content-Type": "application/json",
      "X-Echo-Server": "true"
    }
  });
});

console.log(`Echo server running at http://${host}:${port}/`);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Echo server received SIGTERM");
  server.shutdown();
  Deno.exit(0);
});