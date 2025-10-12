#!/usr/bin/env -S deno run --allow-net

const [host, port] = Deno.args;

let cnt = 0;

const server = Deno.serve({
  hostname: host === "localhost" ? "127.0.0.1" : host,
  port: parseInt(port),
  onListen({ hostname, port }) {
    console.log(`Listening on http://${hostname}:${port}`);
  },
}, (req) => new Response(`hello ${++cnt}\n`));

