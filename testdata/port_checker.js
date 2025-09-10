#!/usr/bin/env -S deno run --allow-net
// Server that verifies it received correct port and can bind to it

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: port_checker.js <host> <port>");
  Deno.exit(1);
}

const expectedPort = parseInt(port);

try {
  const server = Deno.serve({ 
    hostname: host, 
    port: expectedPort 
  }, (req) => {
    return new Response(`Port checker server
Expected host: ${host}
Expected port: ${expectedPort}
Actual host: ${host}
Actual port: ${expectedPort}
URL: ${req.url}
Status: Port binding successful
`, {
      headers: { "Content-Type": "text/plain" }
    });
  });

  console.log(`Port checker successfully bound to http://${host}:${expectedPort}/`);

  // Graceful shutdown
  Deno.addSignalListener("SIGTERM", () => {
    console.log("Port checker received SIGTERM");
    server.shutdown();
    Deno.exit(0);
  });

} catch (error) {
  console.error(`Port checker failed to bind to ${host}:${expectedPort}:`, error.message);
  Deno.exit(1);
}